package pixelproxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/danjacques/pixelproxy/applications/pixelproxy/storage"
	"github.com/danjacques/pixelproxy/applications/pixelproxy/web"
	"github.com/danjacques/pixelproxy/util"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/danjacques/gopushpixels/device"
	"github.com/danjacques/gopushpixels/discovery"
	"github.com/danjacques/gopushpixels/protocol"
	"github.com/danjacques/gopushpixels/proxy"
	"github.com/danjacques/gopushpixels/replay"
	"github.com/danjacques/gopushpixels/replay/streamfile"
	"github.com/danjacques/gopushpixels/support/network"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// Application-level flag variables.
var (
	app = util.Application{
		Verbosity:    zap.WarnLevel,
		Production:   true,
		ColorizeLogs: true,
	}

	interfaceName        = ""
	discoveryAddress     = ""
	discoveryExpiration  = time.Minute
	proxyAddress         = ""
	proxyDiscoveryPeriod = time.Second
	proxyGroupOffset     = int32(0)

	playbackMaxLagAge       = 100 * time.Millisecond
	playbackAutoResumeDelay = time.Duration(0)

	httpAddr        = ":80"
	httpCacheAssets = true

	storagePath                  = filepath.Join(os.TempDir(), "pixelproxy")
	storageWriteCompression      = streamfile.CompressionFlag(streamfile.Compression_SNAPPY)
	storageWriteCompressionLevel = -1

	enableSnapshot     = false
	snapshotSampleRate = 2 * time.Second
)

func init() {
	// Register external monitoring.
	proxy.RegisterMonitoring(prometheus.DefaultRegisterer)
	replay.RegisterMonitoring(prometheus.DefaultRegisterer)

	// Set up root command.
	pf := rootCmd.PersistentFlags()

	app.AddFlags(pf)

	pf.StringVar(&interfaceName, "interface", interfaceName,
		"Name of the network interface to use. If empty, an interface will be chosen.")

	pf.StringVar(&discoveryAddress, "discovery_address", discoveryAddress,
		"Local address to listen on for discovery. If empty, listen on default address.")

	pf.DurationVar(&discoveryExpiration, "discovery_expiration", discoveryExpiration,
		"Period of non-communication before expiring a discovered device.")

	pf.StringVar(&proxyAddress, "proxy_address", proxyAddress,
		"The network [ADDR][:PORT] that proxy devices should identify as. You probably "+
			"do NOT want to supply a port, as that effectively restricts to a single proxy "+
			"device.")

	pf.DurationVar(&proxyDiscoveryPeriod, "proxy_discovery_period", proxyDiscoveryPeriod,
		"Period in between proxy device discovery broadcasts.")

	pf.Int32Var(&proxyGroupOffset, "proxy_group_offset", proxyGroupOffset,
		"A fixed offset to add to a base device's group when calculating the proxy device's "+
			"group identifier. This can be used to differentiate proxy devices while maintaining "+
			"relative group ordering.")

	pf.DurationVar(&playbackMaxLagAge, "playback_max_lag_age", playbackMaxLagAge,
		"The maximum amount of time that a packet can lag behind realtime before we "+
			"discard it. This is used as a fudge factor.")

	pf.DurationVar(&playbackAutoResumeDelay, "playback_auto_resume_delay", playbackAutoResumeDelay,
		"The amount of time after (a) playback has been paused, and (b) the proxy has received "+
			"at least one packet since then that we automatically resume the playback stream.")

	pf.StringVar(&httpAddr, "http_addr", httpAddr, "The HTTP [ADDR]:PORT to listen on.")

	pf.BoolVar(&httpCacheAssets, "http_cache_assets", httpCacheAssets,
		"Cache web assets after loading. Can be disabled for development.")

	pf.StringVar(&storagePath, "storage_path", storagePath, "The file storage path.")

	pf.Var(&storageWriteCompression, "storage_write_compression",
		"Type of compression The file storage path, from: "+streamfile.CompressionFlagValues())

	pf.IntVar(&storageWriteCompressionLevel, "storage_write_compression_level", storageWriteCompressionLevel,
		"If enabled/supported, the compression level to use. <0 means default level, the higher "+
			"the number the more CPU is used to achieve better compression.")

	pf.BoolVar(&enableSnapshot, "enable_snapshot", enableSnapshot,
		"Enable in-memory snapshot of data sent to devices, allowing previews.")

	pf.DurationVar(&snapshotSampleRate, "snapshot_sample_rate", snapshotSampleRate,
		"The rate at which pixel data will be snapshotted.")
}

var rootCmd = &cobra.Command{
	Use:   "pixelproxy",
	Short: "Proxy application for PixelPushers",
	Long:  ``, // TODO: Fill in long descrpition.
	Run: func(cmd *cobra.Command, args []string) {
		app.Run(context.Background(), func(c context.Context) error {
			return rootCmdRun(c, cmd, args)
		})
	},
}

// Execute runs the main application code.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rootCmdRun(c context.Context, cmd *cobra.Command, args []string) (appErr error) {
	// Resolve our discovery broadcast network addresses.
	var discoveryAddr *network.ResolvedConn
	if discoveryAddress != "" {
		var err error
		discoveryAddr, err = network.ResolveUDPAddress(network.AddressOptions{
			Interface:     interfaceName,
			TargetAddress: discoveryAddress,
			Multicast:     true,
		})
		if err != nil {
			logging.S(c).Errorf("Could not resolve discovery address: %s", err)
			return err
		}
	} else {
		discoveryAddr = discovery.DefaultListenerConn()
	}
	logging.S(c).Infof("Using discovery address %q.", discoveryAddr)

	// Resolve our proxy network addresses.
	proxyAddr, err := network.ResolveUDPAddress(network.AddressOptions{
		Interface:     interfaceName,
		TargetAddress: proxyAddress,
		Multicast:     false,
	})
	if err != nil {
		logging.S(c).Errorf("Could not resolve proxy network: %s", err)
		return err
	}
	logging.S(c).Infof("Using proxy address %q.", proxyAddr)

	// Initialize our file storage.
	storage := storage.S{
		Root:                   storagePath,
		WriterCompression:      storageWriteCompression.Value(),
		WriterCompressionLevel: storageWriteCompressionLevel,
	}
	if err := storage.Prepare(c); err != nil {
		logging.S(c).Errorf("Could not create storage root directory %q: %s", storage.Root, err)
		return err
	}

	// Allow our processes to cancel the Context if something goes wrong.
	c, cancelFunc := context.WithCancel(c)
	defer cancelFunc()

	// Roll our process errors into our return value, if applicable.
	var errorMu sync.Mutex
	errorCount := 0
	defer func() {
		if appErr == nil && errorCount > 0 {
			appErr = errors.Errorf("Encountered %d internal error(s).", errorCount)
		}
	}()

	// (Helper function to complete an operation.)
	operationFinished := func(name string, err error) {
		if err == nil {
			logging.S(c).Debugf("Process %s has finished.", name)
			return
		}

		if errors.Cause(err) == context.Canceled {
			logging.S(c).Debugf("Process %s was cancelled.", name)
			return
		}

		// Cancel any other processes.
		cancelFunc()

		logging.S(c).Errorf("Process %s encountered an error: %s", name, err)

		errorMu.Lock()
		defer errorMu.Unlock()
		errorCount++
	}

	// Track registered devices.
	var reg device.Registry

	// A Router to use for routing packets to devices. It will contain
	// registrations for all discovered devices.
	router := device.Router{
		Registry: &reg,
		Logger:   logging.S(c),
	}
	defer router.Shutdown()

	// We will start multiple goroutines. Each will release its WaitGroup when
	// finished. We'll wait for that.
	var processWG sync.WaitGroup
	defer processWG.Wait()

	// Runs a function in a goroutine, counting the error when finished.
	startOperation := func(name string, fn func() error) {
		processWG.Add(1)

		logging.S(c).Debugf("Starting %s...", name)
		go func() {
			defer processWG.Done()
			operationFinished(name, fn())
		}()
	}

	// Manage proxy devices.
	proxyManager := proxy.Manager{
		AddressRegistry: proxy.AddressRegistry{
			// TODO: If we open-source this, do something a little less silly.
			Prefix: []byte{0xE1, 0x2E, 0xC7},
		},
		ProxyAddr:   proxyAddr.Addr.IP,
		GroupOffset: proxyGroupOffset,
		Logger:      logging.S(c),
	}
	defer func() {
		operationFinished("Proxy manager", proxyManager.Close())
	}()

	// Keep a snapshot of proxy strip states.
	var snapshots *device.SnapshotManager
	if enableSnapshot {
		// Our packets come from two places:
		// 1) Packets sent to proxies, which are routed to devices.
		// 2) Packets sent directly to devices through our Router.
		//
		// Note that the Proxy does NOT use the Router, so we need to intercept
		// packets in both places to get the latest snapshots at all times.
		snapshots = &device.SnapshotManager{
			// Sample at a rate faster than our render refresh (currently 5s).
			SampleRate: snapshotSampleRate,
		}

		// Listen for packets received by the proxy.
		proxyManager.AddListener(proxy.ListenerFunc(func(d device.D, pkt *protocol.Packet, forwarded bool) {
			// Only add this packet to the snapshot if it was forwarded to the device.
			// If it was dropped (e.g., during playback), it isn't representative of
			// the current device state.
			if forwarded {
				snapshots.HandlePacket(d, pkt)
			}
		}))

		// Listen for packets sent to our Router. This occurs for playback packets.
		router.AddListener(device.ListenerFunc(snapshots.HandlePacket))
	}

	// Discovery transmitter for our proxy devices.
	proxyTransmitterSender := network.ResilientDatagramSender{
		Factory: discovery.DefaultTransmitterConn().DatagramSender,
	}
	defer func() {
		operationFinished("Proxy discovery transmitter", proxyTransmitterSender.Close())
	}()

	proxyTransmitter := discovery.Transmitter{
		Logger: logging.S(c),
	}

	startOperation("Discovery broadcast", func() error {
		return util.LoopUntil(c, proxyDiscoveryPeriod, func(c context.Context) error {
			devices := proxyManager.ProxyDevices()
			logging.S(c).Debugf("Broadcasting discovery for %d proxy device(s)...", len(devices))
			for _, d := range devices {
				if err := proxyTransmitter.Broadcast(&proxyTransmitterSender, d.DiscoveryHeaders()); err != nil {
					logging.S(c).Warnf("Failed to broadcast discovery for proxy device %q: %s", d, err)
				}
			}
			return nil
		})
	})

	// Set up discovery.
	discoveryConn, err := discoveryAddr.ListenMulticastUDP4()
	if err != nil {
		logging.S(c).Errorf("Failed to create discovery listener: %s", err)
		return err
	}

	l := discovery.Listener{
		Logger: logging.S(c),

		// Filter any proxy device addresses, so we don't end up proxying our own
		// proxies.
		FilterFunc: func(dh *protocol.DiscoveryHeaders) bool {
			return !proxyManager.IsProxyDeviceAddr(dh.HardwareAddr())
		},
	}
	if err := l.Start(discoveryConn); err != nil {
		logging.S(c).Errorf("Could not connect discovery listener to %q: %s", discoveryConn.LocalAddr(), err)
		discoveryConn.Close()
		return err
	}
	defer func() {
		if err := l.Close(); err != nil {
			logging.S(c).Warnf("Could not close discovery listener: %s", err)
		}
	}()

	discoveryReg := discovery.Registry{
		Expiration:     discoveryExpiration,
		DeviceRegistry: &reg,
	}
	defer discoveryReg.Shutdown()

	// Listen on our discovery address for advertised devices.
	startOperation("Discovery listener", func() error {
		return discovery.ListenAndRegister(c, &l, &discoveryReg, func(d device.D) error {
			// Add the device to our proxy manager. This will cause a proxy device
			// to be created for it.
			if err := proxyManager.AddDevice(d); err != nil {
				logging.S(c).Errorf("Could not create proxy for device %s: %s", d, err)
			}
			return nil
		})
	})

	// Initialize and run our Controller. This will block for the lifetime of the
	// application.
	ctrl := Controller{
		Router:            &router,
		DiscoveryRegistry: &discoveryReg,
		ProxyManager:      &proxyManager,
		Snapshots:         snapshots,
		Storage:           &storage,
		ShutdownFunc:      cancelFunc,
		PlaybackMaxLagAge: playbackMaxLagAge,
		AutoResumeDelay:   playbackAutoResumeDelay,
	}

	// Start our HTTP server.
	webMux := mux.NewRouter()

	// Install profiling endpoints.
	app.Profiler.AddHTTP(webMux)

	// Setup our Prometheus HTTP handler.
	webMux.Path("/metrics").Handler(promhttp.Handler())

	webController := web.Controller{
		Proxy:                 &ctrl,
		CacheAssets:           httpCacheAssets,
		Logger:                logging.L(c),
		RenderRefreshInterval: time.Duration(2.5 * float64(snapshotSampleRate)),
	}
	if err := webController.Install(c, webMux); err != nil {
		logging.S(c).Errorf("Failed to install HTTP routes: %s", err)
		return err
	}

	webServer := http.Server{
		Addr:    httpAddr,
		Handler: webMux,
	}

	startOperation("web server", func() error {
		// Shutdown our web server when our Context is cancelled.
		go func() {
			<-c.Done()
			if err := webServer.Shutdown(c); err != nil {
				logging.S(c).Warnf("Error during web server shutdown: %s", err)
			}
		}()

		logging.S(c).Infof("Serving HTTP on %q", webServer.Addr)
		if err := webServer.ListenAndServe(); err != nil {
			if errors.Cause(err) != http.ErrServerClosed {
				return err
			}
		}
		return nil
	})

	// Run our Controller.
	if err := ctrl.Run(c); err != nil {
		if errors.Cause(err) == context.Canceled {
			logging.S(c).Debugf("Canceled while running Controller: %s", err)
		} else {
			logging.S(c).Errorf("Error while running Controller: %s", err)
		}
		return err
	}
	logging.S(c).Infof("Controller has stopped.")

	return nil
}
