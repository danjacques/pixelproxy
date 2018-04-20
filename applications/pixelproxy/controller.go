package pixelproxy

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danjacques/pixelproxy/applications/pixelproxy/storage"
	"github.com/danjacques/pixelproxy/applications/pixelproxy/web"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/danjacques/gopushpixels/device"
	"github.com/danjacques/gopushpixels/discovery"
	"github.com/danjacques/gopushpixels/protocol"
	"github.com/danjacques/gopushpixels/proxy"
	"github.com/danjacques/gopushpixels/replay"
	"github.com/danjacques/gopushpixels/replay/streamfile"

	"github.com/golang/protobuf/ptypes"
	"github.com/pkg/errors"
)

// errNotRunning is an error returned by Controller methods that are called
// while the Controller isn't currently blocked in its Run method.
var errNotRunning = errors.New("controller is not running")

// Controller controls the operational state of the application.
type Controller struct {
	// Storage manages the underlying storage filesystem.
	Storage *storage.S

	// Router is the router to use for packet routing.
	Router *device.Router

	// DiscoveryRegistry is a registry of discovered devices.
	DiscoveryRegistry *discovery.Registry
	// ProxyManager manages the device proxy state.
	ProxyManager *proxy.Manager

	// Snapshots, if not nil, is the snapshot manager for registered devices.
	Snapshots *device.SnapshotManager

	// ShutdownFunc is a function that can be called to shutdown the system,
	// cancelling its outer Context.
	ShutdownFunc context.CancelFunc

	// PlaybackMaxLagAge is the MaxLagAge value to provide to our Player.
	PlaybackMaxLagAge time.Duration

	// AutoResumeDelay, if >0, is the amount of time after (a) the Controller has
	// been paused, and (b) the ProxyManager has received a packet, after which
	// the Controller will automatically resume.
	AutoResumeDelay time.Duration

	// ctx is this Controller's Context, passed to its Run method.
	ctx context.Context

	// startTime is when the Controller started.
	startTime time.Time

	// All of the following is protected by the Mutex.
	mu            sync.Mutex
	systemControl *SystemControl

	player             *replay.Player
	playingName        string
	autoResumeListener *proxy.AutoResumeListener

	recorder         *replay.Recorder
	recorderListener proxy.Listener
	recordingName    string

	hasProxyManagerLease bool

	// isRunning is a protected value that will be true if the Controller is
	// currently running.
	isRunning bool
}

var _ web.ControllerProxy = (*Controller)(nil)

func (ctrl *Controller) running() bool {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	return ctrl.isRunning
}

// Run runs the Controller until its Context is cancelled.
func (ctrl *Controller) Run(c context.Context) error {
	// Load the default filename.
	defaultFileName, err := ctrl.Storage.GetDefault()
	if err != nil {
		return errors.Wrap(err, "loading default file")
	}

	// Mark that we're running.
	func() {
		ctrl.mu.Lock()
		defer ctrl.mu.Unlock()

		if ctrl.isRunning {
			panic("controller is already running")
		}

		// Initialize our starting state.
		ctrl.ctx = c
		ctrl.isRunning = true
		ctrl.startTime = time.Now()
		ctrl.systemControl = DefaultSystemControl
		ctrl.stopTaskLocked()
	}()

	// Before we quit, shut down any ongoing operations.
	defer func() {
		ctrl.mu.Lock()
		defer ctrl.mu.Unlock()

		// Remove any ProxyManager lease.
		ctrl.ProxyManager.RemoveLease(ctrl)

		// Stop any ongoing operations.
		ctrl.stopTaskLocked()

		// Mark that we're no longer running.
		ctrl.ctx = nil
		ctrl.isRunning = false
	}()

	// If we have a default file, begin playback on it.
	if defaultFileName != "" {
		logging.S(c).Infof("Playing defualt file %q...", defaultFileName)
		if err := ctrl.PlayFile(c, defaultFileName); err != nil {
			logging.S(c).Warnf("Failed to play default file %q: %s", defaultFileName, err)
		}
	}

	// Wait until our Context is cancelled.
	<-c.Done()
	return nil
}

// HandlePacket offers a captured packet to the controller for handling.
func (ctrl *Controller) HandlePacket(c context.Context, id string, packet []byte) {
	// This can happen if our subsystems receive and handle packets before the
	// Controller is initialized and running.
	if !ctrl.running() {
		return
	}
}

// Status implements web.ControllerProxy.
func (ctrl *Controller) Status() web.ControllerStatus {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// Build as much of status as we can without holding a lock.
	status := web.ControllerStatus{
		StartTime:                ctrl.startTime,
		Uptime:                   time.Now().Sub(ctrl.startTime),
		ProxyForwarding:          ctrl.ProxyManager.Forwarding(),
		DisablingProxyForwarding: ctrl.hasProxyManagerLease,
	}

	if ctrl.player != nil {
		if v := ctrl.player.Status(); v != nil {
			status.PlaybackStatus = &web.PlaybackStatus{
				Name:          filepath.Base(v.Path),
				Rounds:        v.Rounds,
				Position:      v.Position,
				Duration:      v.Duration,
				TotalPlaytime: v.TotalPlaytime,
				Paused:        v.Paused,
			}

			status.PlaybackStatus.NoRouteDevices = make([]string, len(v.NoRouteDevices))
			for i, e := range v.NoRouteDevices {
				var noRouteStr string
				if e.Ordinal.IsValid() {
					noRouteStr = fmt.Sprintf("{%d, %d} %s (%d)", e.Ordinal.Group, e.Ordinal.Controller, e.ID, e.Count)
				} else {
					noRouteStr = fmt.Sprintf("%s (%d)", e.ID, e.Count)
				}
				status.PlaybackStatus.NoRouteDevices[i] = noRouteStr
			}
			sort.Strings(status.PlaybackStatus.NoRouteDevices)

			// Calculate the percentage, if safe.
			if v.Duration > 0 && v.Position < v.Duration {
				status.PlaybackStatus.Progress = int(float64(v.Position) / float64(v.Duration) * 100)
			}
		} else {
			// Recorder is not nil, but also not returning a status. Mark that we're
			// recording.
			status.PlaybackStatus = &web.PlaybackStatus{}
		}
	}

	if ctrl.recorder != nil {
		if v := ctrl.recorder.Status(); v != nil {
			status.RecordStatus = &web.RecordStatus{
				Name:     filepath.Base(v.Name),
				Events:   v.Events,
				Bytes:    v.Bytes,
				Duration: v.Duration,
			}
			if v.Error != nil {
				status.RecordStatus.Error = v.Error.Error()
			}
		} else {
			// Recorder is not nil, but also not returning a status. Mark that we're
			// recording.
			status.RecordStatus = &web.RecordStatus{}
		}
	}

	return status
}

// ListFiles implements web.ControllerProxy.
func (ctrl *Controller) ListFiles(c context.Context) (*web.FileList, error) {
	if !ctrl.running() {
		return nil, errNotRunning
	}

	// Get the default file name.
	defaultFileName, err := ctrl.Storage.GetDefault()
	if err != nil {
		return nil, err
	}

	files, err := ctrl.Storage.ListFiles(c)
	if err != nil {
		return nil, err
	}

	webFiles := make([]*web.File, len(files))
	for i, f := range files {
		var maxStrips, maxPixelsPerStrip int64
		for _, d := range f.Metadata.Devices {
			if d.PixelsPerStrip > maxPixelsPerStrip {
				maxPixelsPerStrip = d.PixelsPerStrip
			}
			if v := int64(len(d.Strip)); v > maxStrips {
				maxStrips = v
			}
		}

		// Determine compression.
		comps := make(map[streamfile.Compression]struct{})
		for _, efi := range f.Metadata.EventFileInfo {
			comps[efi.Compression] = struct{}{}
		}
		allComps := make([]string, 0, len(comps))
		for k := range comps {
			allComps = append(allComps, k.String())
		}
		sort.Strings(allComps)

		wf := web.File{
			Name:              f.DisplayName,
			NumDevices:        len(f.Metadata.Devices),
			MaxStrips:         int(maxStrips),
			MaxPixelsPerStrip: int(maxPixelsPerStrip),
			DiskBytes:         f.Size,
			NumBytes:          f.Metadata.NumBytes,
			NumEvents:         f.Metadata.NumEvents,
			Compression:       strings.Join(allComps, " "),
			IsDefault:         f.DisplayName == defaultFileName,
		}

		wf.Created, _ = ptypes.Timestamp(f.Metadata.Created)
		wf.Created = wf.Created.Local()
		wf.Duration, _ = ptypes.Duration(f.Metadata.Duration)

		webFiles[i] = &wf
	}
	sort.Slice(webFiles, func(i, j int) bool { return webFiles[i].Name < webFiles[j].Name })

	return &web.FileList{
		DefaultFileName: defaultFileName,
		Files:           webFiles,
	}, nil
}

// Devices implements web.ControllerProxy.
func (ctrl *Controller) Devices() []*web.DeviceInfo {
	discoveredDevices := ctrl.DiscoveryRegistry.Devices()
	proxyDevices := ctrl.ProxyManager.ProxyDevices()
	allInfo := make([]*web.DeviceInfo, 0, len(discoveredDevices)+len(proxyDevices))

	commonInfo := func(d device.D, t string) *web.DeviceInfo {
		dh := d.DiscoveryHeaders()
		info := d.Info()

		di := web.DeviceInfo{
			Type:            t,
			ID:              d.ID(),
			BytesReceived:   info.BytesReceived,
			PacketsReceived: info.PacketsReceived,
			BytesSent:       info.BytesSent,
			PacketsSent:     info.PacketsSent,
			Created:         info.Created,
			LastObserved:    info.Observed,
			HasSnapshot:     ctrl.Snapshots != nil && ctrl.Snapshots.HasSnapshotForDevice(d),
		}

		if addr := d.Addr(); addr != nil {
			di.Network = addr.Network()
			di.Address = addr.String()
		}

		if pp := dh.PixelPusher; pp != nil {
			di.Strips = int(pp.StripsAttached)
			di.Pixels = int(pp.PixelsPerStrip)
			di.Group = int(pp.GroupOrdinal)
			di.Controller = int(pp.ControllerOrdinal)
		}

		return &di
	}

	// Discovered device info.
	for _, d := range discoveredDevices {
		allInfo = append(allInfo, commonInfo(d, "discovered"))
	}
	for _, d := range proxyDevices {
		di := commonInfo(d, "proxy")
		di.ProxiedID = d.Proxied().ID()
		allInfo = append(allInfo, di)
	}

	// Sort the devices in order: Group < Controller < Type < ProxiedID < ID
	sort.Slice(allInfo, func(i, j int) bool {
		di, dj := allInfo[i], allInfo[j]
		switch {
		case di.Group < dj.Group:
			return true
		case di.Group > dj.Group:
			return false

		case di.Controller < dj.Controller:
			return true
		case di.Controller > dj.Controller:
			return false

		case di.Type < dj.Type:
			return true
		case di.Type > dj.Type:
			return false

		case di.ProxiedID < dj.ProxiedID:
			return true
		case di.ProxiedID > dj.ProxiedID:
			return false

		default:
			return di.ID < dj.ID
		}
	})

	return allInfo
}

// RecordFile implements web.ControllerProxy.
func (ctrl *Controller) RecordFile(c context.Context, name string) error {
	logging.S(c).Infof("Begininning recording for: %q", name)
	if !ctrl.running() {
		return errNotRunning
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// Stop the current operation, if one is running.
	ctrl.stopTaskLocked()

	// Open our output file.
	sw, err := ctrl.Storage.OpenWriter(name)
	if err != nil {
		logging.S(c).Errorf("could not open output file %q: %s", name, err)
		return err
	}

	// Create a Recorder and have it receive proxied data.
	ctrl.recorder = &replay.Recorder{}
	ctrl.recorderListener = proxy.ListenerFunc(func(d device.D, pkt *protocol.Packet, forwarding bool) {
		switch err := ctrl.recorder.RecordPacket(d, pkt); errors.Cause(err) {
		case nil:

		case streamfile.ErrEncodingNotSupported:
			// We are tolerant of unsupported encoding errors.
			logging.S(c).Warnf("Unsupported encoding for packet from device %q: %s", d.ID(), pkt)

		default:
			logging.S(c).Warnf("Error recording packet %s for device %q: %s", d.ID(), pkt, err)
			// Detach our Listener, since there's no point in receiving more packets.
			ctrl.ProxyManager.RemoveListener(ctrl.recorderListener)

			// Try and stop our recorder. This will provide a more accurate user
			// experience, since the recorder state will be shown to be stopped.
			_ = ctrl.Stop(ctrl.ctx)
		}
	})
	ctrl.recordingName = name

	// Start our recorder. It will take ownership of sw.
	ctrl.recorder.Start(sw)
	// Hook our recorder up to our proxy manager so it can record packets that the
	// proxy receives.
	ctrl.ProxyManager.AddListener(ctrl.recorderListener)
	return nil
}

// MergeFiles implements web.ControllerProxy.
func (ctrl *Controller) MergeFiles(c context.Context, name string, srcs ...string) error {
	logging.S(c).Infof("Merging %d file(s) into %q: %v", len(srcs), name, srcs)

	if len(srcs) == 0 {
		return errors.New("no source files")
	}

	// Merging is actually independent, so we can do it without stopping any
	// operations or locking. Of course, it could fail, but...
	return ctrl.Storage.MergeFiles(name, srcs)
}

// Stop implements web.ControllerProxy.
func (ctrl *Controller) Stop(c context.Context) error {
	if !ctrl.running() {
		return errNotRunning
	}

	logging.S(c).Info("Received stop command.")

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// Stop the current operation, if one is running.
	ctrl.stopTaskLocked()
	return nil
}

// PlayFile implements web.ControllerProxy.
func (ctrl *Controller) PlayFile(c context.Context, name string) error {
	logging.S(c).Infof("Playing file: %q", name)
	if !ctrl.running() {
		return errNotRunning
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// Stop any current operation, if one is running.
	ctrl.stopTaskLocked()

	sr, err := ctrl.Storage.OpenReader(name)
	if err != nil {
		logging.S(c).Errorf("Could not open %q for playback: %s", name, err)
		return err
	}

	// Create a player and run it.
	ctrl.player = &replay.Player{
		SendPacket: func(ord device.Ordinal, id string, pkt *protocol.Packet) error {
			return ctrl.Router.Route(ord, id, pkt)
		},
		PlaybackLeaser: &proxyManagerPlaybackLeaser{ctrl.ProxyManager},
		MaxLagAge:      ctrl.PlaybackMaxLagAge,
		Logger:         logging.S(ctrl.ctx),
	}
	ctrl.playingName = name

	// Start playback.
	ctrl.player.Play(ctrl.ctx, sr)

	return nil
}

// PauseFile implements web.ControllerProxy.
func (ctrl *Controller) PauseFile(c context.Context) error {
	logging.S(c).Infof("Pausing file...")
	if !ctrl.running() {
		return errNotRunning
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	if ctrl.player != nil {
		ctrl.player.Pause()
	}

	// Add an auto-resume listener, if we don't already have one.
	if ctrl.autoResumeListener == nil && ctrl.AutoResumeDelay > 0 {
		ctrl.autoResumeListener = &proxy.AutoResumeListener{
			ProxyManager: ctrl.ProxyManager,
			OnDelay: func(c context.Context) {
				if err := ctrl.ResumeFile(c); err != nil {
					logging.S(c).Warnf("Failed to auto-resume playback: %s", err)
				}
			},
			Delay:  ctrl.AutoResumeDelay,
			Logger: logging.S(ctrl.ctx),
		}
		ctrl.autoResumeListener.Start(ctrl.ctx)
	}

	return nil
}

// ResumeFile implements web.ControllerProxy.
func (ctrl *Controller) ResumeFile(c context.Context) error {
	logging.S(c).Infof("Resuming file...")
	if !ctrl.running() {
		return errNotRunning
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// If we have an auto-resume listener, stop it now, since we're manually
	// resuming.
	if ctrl.autoResumeListener != nil {
		ctrl.autoResumeListener.Stop()
		ctrl.autoResumeListener = nil
	}

	if ctrl.player != nil {
		ctrl.player.Resume()
	}

	return nil
}

// DeleteFile implements web.ControllerProxy.
func (ctrl *Controller) DeleteFile(c context.Context, name string) error {
	logging.S(c).Infof("Deleting file: %q", name)
	if !ctrl.running() {
		return errNotRunning
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// If we're currently recording or playing this file, stop.
	if ctrl.recorder != nil && ctrl.recordingName == name {
		ctrl.stopTaskLocked()
	}
	if ctrl.player != nil && ctrl.playingName == name {
		ctrl.stopTaskLocked()
	}

	return ctrl.Storage.DeleteFile(name)
}

// Strips implements web.ControllerProxy.
func (ctrl *Controller) Strips(c context.Context, deviceName string) ([]web.Strip, error) {
	if ctrl.Snapshots == nil {
		return nil, nil
	}

	var d device.D
	for _, rd := range ctrl.DiscoveryRegistry.Devices() {
		if rd.ID() == deviceName {
			d = rd
			break
		}
	}
	if d == nil {
		logging.S(c).Infof("No device registered for: %q", deviceName)
		return nil, nil
	}

	// Get the snapshot for this device.
	snapshot := ctrl.Snapshots.SnapshotForDevice(d)
	if snapshot == nil {
		return nil, nil
	}

	// Convert it into a web snapshot.
	strips := make([]web.Strip, len(snapshot.Strips))
	for i, strip := range snapshot.Strips {
		ws := &strips[i]
		ws.Number = int(strip.StripNumber)
		ws.Pixels = make([]web.Pixel, 0, strip.Pixels.Len())

		for i := 0; i < strip.Pixels.Len(); i++ {
			pixel := strip.Pixels.Pixel(i)
			ws.Pixels = append(ws.Pixels, web.Pixel{
				R: pixel.Red,
				G: pixel.Green,
				B: pixel.Blue,
			})
		}
	}
	return strips, nil
}

// SetDefaultFile implements web.ControllerProxy.
func (ctrl *Controller) SetDefaultFile(c context.Context, name string) error {
	if !ctrl.running() {
		return errNotRunning
	}

	if name == "" {
		logging.S(c).Infof("Clearing default file.")
	} else {
		logging.S(c).Infof("Setting default file to: %q", name)
	}
	return ctrl.Storage.SetDefault(name)
}

// SetProxyForwarding enables or disables Controller-level block on proxy
// forwarding.
func (ctrl *Controller) SetProxyForwarding(c context.Context, forward bool) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	if forward {
		// Remove any lease we may have.
		logging.S(c).Infof("Controller unblocking proxy forwarding...")
		ctrl.ProxyManager.RemoveLease(ctrl)
	} else {
		// Take out a lease, disabling forwarding until we return it.
		logging.S(c).Infof("Controller blocking proxy forwarding...")
		ctrl.ProxyManager.AddLease(ctrl)
	}
	ctrl.hasProxyManagerLease = !forward

	return nil
}

// SystemState implements web.ControllerProxy.
func (ctrl *Controller) SystemState(c context.Context) *web.SystemState {
	if err := ctrl.systemControl.ValidateAccess(c); err != nil {
		return &web.SystemState{
			Status: fmt.Sprintf("Improperly Configured: %s", err),
		}
	}

	return &web.SystemState{
		Status: "Working",
	}
}

// Shutdown implements web.ControllerProxy.
func (ctrl *Controller) Shutdown(c context.Context, restart bool) error {
	logging.S(c).Warnf("Received shutdown command (restart=%v), beginning shutdown...", restart)

	if err := ctrl.Stop(c); err != nil {
		logging.S(c).Errorf("Failed to stop running tasks; shutting down anyway: %s", err)
	}

	if restart {
		return ctrl.systemControl.Restart(c)
	}
	return ctrl.systemControl.Shutdown(c)
}

// stopTaskLocked shuts down the current Recorder, ending its operation.
func (ctrl *Controller) stopTaskLocked() {
	if ctrl.player != nil {
		logging.S(ctrl.ctx).Infof("Stopping player.")
		ctrl.player.Stop()
		ctrl.player = nil
		ctrl.playingName = ""
	}

	if ctrl.autoResumeListener != nil {
		logging.S(ctrl.ctx).Infof("Stopping auto-resume listener.")
		ctrl.autoResumeListener.Stop()
		ctrl.autoResumeListener = nil
	}

	if ctrl.recorderListener != nil {
		ctrl.ProxyManager.RemoveListener(ctrl.recorderListener)
		ctrl.recorderListener = nil
	}
	if ctrl.recorder != nil {
		logging.S(ctrl.ctx).Infof("Stopping recorder.")
		if err := ctrl.recorder.Stop(); err != nil {
			logging.S(ctrl.ctx).Warnf("Failed to stop recorder.")
		}

		ctrl.recorder = nil
		ctrl.recordingName = ""
	}
}

// proxyManagerPlaybackLeaser is a replay.PlaybackLeaser implementation that
// suppresses the ProxyManager's routing.
type proxyManagerPlaybackLeaser struct {
	pm *proxy.Manager
}

func (l *proxyManagerPlaybackLeaser) AcquirePlaybackLease() { l.pm.AddLease(l) }
func (l *proxyManagerPlaybackLeaser) ReleasePlaybackLease() { l.pm.RemoveLease(l) }
