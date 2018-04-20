package web

import (
	"context"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/danjacques/pixelproxy/applications/pixelproxy/web/assets"
	"github.com/danjacques/pixelproxy/util/logging"
	"github.com/danjacques/pixelproxy/web"
	"github.com/danjacques/pixelproxy/web/bootstrap"

	"github.com/NYTimes/gziphandler"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ControllerProxy defines a set of functions that the Controller can serve
// from.
type ControllerProxy interface {
	// State returns the current state of the controller proxy.
	Status() ControllerStatus

	// ListFiles returns a list of all of the files currently stored on disk.
	ListFiles(c context.Context) (*FileList, error)

	// Devices returns a list of devices that are currently connected.
	Devices() []*DeviceInfo

	// SystemState polls and returns the system state.
	SystemState(context.Context) *SystemState

	// Stop ends the current operation (recording or playback). If no operation
	// is ongoing, Stop does nothing.
	Stop(c context.Context) error

	// RecordFile begins recording proxied data to a File named "name".
	RecordFile(c context.Context, name string) error

	// MergeFiles merges the contents of srcs together into a new file called
	// name.
	MergeFiles(c context.Context, name string, srcs ...string) error

	// PlayFile begins the playback of the named file through the proxy.
	PlayFile(c context.Context, name string) error

	// PauseFile pauses the currently-playing file. If nothing is currently
	// playing, PauseFile will return nil.
	PauseFile(c context.Context) error

	// PauseFile pauses the currently-playing file. If nothing is currently
	// playing, ResumeFile will return nil.
	ResumeFile(c context.Context) error

	// DeleteFile deletes the file with the specified name.
	DeleteFile(c context.Context, name string) error

	// Strips returns a snapshot of the strips for the specified device.
	Strips(c context.Context, device string) ([]Strip, error)

	// SetProxyorwarding enables or disables the proxy packet forwarding.
	SetProxyForwarding(c context.Context, forward bool) error

	// SetDefaultFile sets the default (auto-play) file name.
	//
	// If name is empty, this clears the default file if one is set.
	SetDefaultFile(c context.Context, name string) error

	// Shutdown issues a shutdown command to the system.
	//
	// If reboot is true, Shutdown will attempt to reboot the system instead of
	// just powering it down.
	//
	// In both cses, Shutdown rquires local system permission to execute the
	// operation, and will block until the command has been sent.
	Shutdown(c context.Context, reboot bool) error
}

// Controller is an HTTP endpoint set that serves content and endpoints which
// enable the control of a ControllerProxy.
type Controller struct {
	// Proxy is the ControllerProxy to control. It must not be nil.
	Proxy ControllerProxy

	// CacheAssets, if true, indicates that assets should be cached after being
	// loaded.
	CacheAssets bool

	// Logger is the logger instance to use. If nil, no logging will be performed.
	Logger *zap.Logger

	// RenderRefreshInterval, if > 0, is the automatic refresh interval that will
	// be pushed to the device preview render page.
	RenderRefreshInterval time.Duration

	// site is the underlying site.
	site *web.Site
}

// Install installs this Controller into mux.
//
// The website expects the API to be installed at "/".
//
// The specified Context, c, will be used by each Request handler.
func (cont *Controller) Install(c context.Context, r *mux.Router) error {
	// Instantiate our base Site.
	cont.site = &web.Site{
		Logger: cont.Logger,
		Cache:  cont.CacheAssets,
		Roots: map[string]web.AssetLoader{
			"templates": &assets.Templates,
		},
		TemplateFuncMap: defaultTemplateFuncs,
	}

	// Configure our templates.
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}

	must(cont.site.AddTemplate("templates/index.html", "templates/scaffold.html"))
	must(cont.site.AddTemplate("templates/devices.html", "templates/scaffold.html"))
	must(cont.site.AddTemplate("templates/render.html", "templates/scaffold.html"))
	must(cont.site.AddTemplate("templates/system.html", "templates/scaffold.html"))
	must(cont.site.AddTemplate("templates/logs.html", "templates/scaffold.html"))

	// Configure our Router.
	r = r.StrictSlash(false)

	// Monitoring middleware.
	monitorMW := web.MonitoringMiddleware{
		Logger: cont.Logger,
	}

	r.Use(
		// Add a Context to our requests.
		web.WithContextMiddleware(c),

		// Monitor HTTP operations.
		monitorMW.Middleware,

		// Compress our responses.
		gziphandler.GzipHandler,

		// Minify our response data, if possible.
		web.DefaultMinifier().Middleware,
	)

	// Set up API routes.
	apiRouter := r.PathPrefix("/_api").Subrouter()
	cont.addAPIRoutes(apiRouter)

	r.Path("/").HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		http.Redirect(rw, req, "/index.html", http.StatusFound)
	})
	r.Path("/index.html").HandlerFunc(cont.handleIndexTemplate)
	r.Path("/devices.html").HandlerFunc(cont.handleDevicesTemplate("templates/devices.html"))
	r.Path("/render.html").HandlerFunc(cont.handleDevicesTemplate("templates/render.html"))
	r.Path("/system.html").HandlerFunc(cont.handleSystemTemplate)
	r.Path("/all-logs.html").HandlerFunc(cont.handleAllLogsTemplate)
	r.Path("/error-logs.html").HandlerFunc(cont.handleErrorLogsTemplate)
	r.Path("/strips/{device}.svg").Methods("GET").HandlerFunc(cont.handleStripSVG)
	r.PathPrefix("/bs").Handler(http.FileServer(bootstrap.Bundle.Box))
	r.PathPrefix("/").Handler(http.FileServer(assets.WWW.Box))

	return nil
}

func (cont *Controller) addAPIRoutes(r *mux.Router) {
	r.Path("/status").Methods("GET").HandlerFunc(web.HandleJSON(cont.handleAPIStatus))
	r.Path("/listFiles").Methods("GET").HandlerFunc(web.HandleJSON(cont.handleAPIListFiles))
	r.Path("/recordFile/{name}").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIRecordFile))
	r.Path("/mergeFiles/{name}").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIMergeFiles))
	r.Path("/playFile/{name}").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIPlayFile))
	r.Path("/pause").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIPause))
	r.Path("/resume").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIResume))
	r.Path("/deleteFile/{name}").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIDeleteFile))
	r.Path("/setDefault/{name}").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPISetDefaultFile))
	r.Path("/clearDefault").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIClearDefaultFile))
	r.Path("/stop").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIStop))
	r.Path("/proxyForwarding/enable").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIEnableProxyForwarding))
	r.Path("/proxyForwarding/disable").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIDisableProxyForwarding))
	r.Path("/system/reboot").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIReboot))
	r.Path("/system/shutdown").Methods("POST").HandlerFunc(web.HandleJSON(cont.handleAPIShutdown))
}

func (cont *Controller) handleAPIStatus(rw http.ResponseWriter, req *http.Request) interface{} {
	return Status{
		Status:  cont.Proxy.Status(),
		Devices: cont.Proxy.Devices(),
	}
}

func (cont *Controller) handleAPIListFiles(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	files, err := cont.Proxy.ListFiles(c)
	if err != nil {
		cont.Logger.Sugar().Errorf("Failed to list files: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return files
}

func (cont *Controller) handleAPIRecordFile(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	vars := mux.Vars(req)
	name := vars["name"]
	if name == "" {
		rw.WriteHeader(http.StatusBadRequest)
		return errors.New("missing 'name'")
	}

	if err := cont.Proxy.RecordFile(c, name); err != nil {
		cont.Logger.Sugar().Errorf("Failed to record: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIMergeFiles(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	vars := mux.Vars(req)

	// Destination file name (path parameter).
	name := vars["name"]
	if name == "" {
		rw.WriteHeader(http.StatusBadRequest)
		return errors.New("missing 'name'")
	}

	// Grab source names from (potentially repeating) query string.
	srcs := req.URL.Query()["src"]

	if err := cont.Proxy.MergeFiles(c, name, srcs...); err != nil {
		cont.Logger.Sugar().Errorf("Failed to merge: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIPlayFile(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	vars := mux.Vars(req)
	name := vars["name"]
	if name == "" {
		rw.WriteHeader(http.StatusBadRequest)
		return errors.New("missing 'name'")
	}

	if err := cont.Proxy.PlayFile(c, name); err != nil {
		cont.Logger.Sugar().Errorf("Failed to play %q: %s", name, err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIPause(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()

	if err := cont.Proxy.PauseFile(c); err != nil {
		cont.Logger.Sugar().Errorf("Failed to pause: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIResume(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()

	if err := cont.Proxy.ResumeFile(c); err != nil {
		cont.Logger.Sugar().Errorf("Failed to resume: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIDeleteFile(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	vars := mux.Vars(req)
	name := vars["name"]
	if name == "" {
		rw.WriteHeader(http.StatusBadRequest)
		return errors.New("missing 'name'")
	}

	if err := cont.Proxy.DeleteFile(c, name); err != nil {
		cont.Logger.Sugar().Errorf("Failed to delete %q: %s", name, err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPISetDefaultFile(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	vars := mux.Vars(req)
	name := vars["name"]
	if name == "" {
		rw.WriteHeader(http.StatusBadRequest)
		return errors.New("missing 'name'")
	}

	if err := cont.Proxy.SetDefaultFile(c, name); err != nil {
		cont.Logger.Sugar().Errorf("Failed to set default file to %q: %s", name, err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIClearDefaultFile(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()

	if err := cont.Proxy.SetDefaultFile(c, ""); err != nil {
		cont.Logger.Sugar().Errorf("Failed to clear default file: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIStop(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	if err := cont.Proxy.Stop(c); err != nil {
		cont.Logger.Sugar().Errorf("Failed to stop: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIEnableProxyForwarding(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()

	if err := cont.Proxy.SetProxyForwarding(c, true); err != nil {
		cont.Logger.Sugar().Errorf("Failed to enable proxy forwarding: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIDisableProxyForwarding(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()

	if err := cont.Proxy.SetProxyForwarding(c, false); err != nil {
		cont.Logger.Sugar().Errorf("Failed to disable proxy forwarding: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIReboot(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	if err := cont.Proxy.Shutdown(c, true); err != nil {
		cont.Logger.Sugar().Errorf("Failed to reboot: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleAPIShutdown(rw http.ResponseWriter, req *http.Request) interface{} {
	c := req.Context()
	if err := cont.Proxy.Shutdown(c, false); err != nil {
		cont.Logger.Sugar().Errorf("Failed to shutdown: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return err
	}

	return nil
}

func (cont *Controller) handleIndexTemplate(rw http.ResponseWriter, req *http.Request) {
	c := req.Context()

	cont.site.RenderWithError(rw, "text/html", func() error {
		status := cont.Proxy.Status()

		files, err := cont.Proxy.ListFiles(c)
		if err != nil {
			return errors.Wrap(err, "listing files")
		}

		return cont.site.RenderTemplate(rw, "templates/index.html", struct {
			Status          ControllerStatus
			DefaultFileName string
			Files           []*File
		}{
			Status:          status,
			DefaultFileName: files.DefaultFileName,
			Files:           files.Files,
		})
	})
}

func (cont *Controller) handleDevicesTemplate(name string) http.HandlerFunc {
	const defaultRefreshInterval = 5 * time.Second

	refreshInterval := cont.RenderRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = defaultRefreshInterval
	}

	return func(rw http.ResponseWriter, req *http.Request) {
		// Get the current list of devices.
		now := time.Now()
		devices := cont.Proxy.Devices()

		// Do any of our devices have snapshots?
		hasSnapshots := false
		for _, d := range devices {
			if d.HasSnapshot {
				hasSnapshots = true
				break
			}
		}

		cont.site.RenderWithError(rw, "text/html", func() error {
			return cont.site.RenderTemplate(rw, name, struct {
				Devices               []*DeviceInfo
				HasSnapshots          bool
				Now                   time.Time
				RefreshIntervalMillis int64
			}{
				Devices:      devices,
				HasSnapshots: hasSnapshots,
				Now:          now,
				RefreshIntervalMillis: int64(refreshInterval / time.Millisecond),
			})
		})
	}
}

func (cont *Controller) handleSystemTemplate(rw http.ResponseWriter, req *http.Request) {
	c := req.Context()
	state := cont.Proxy.SystemState(c)

	cont.site.RenderWithError(rw, "text/html", func() error {
		return cont.site.RenderTemplate(rw, "templates/system.html", struct {
			State *SystemState
		}{
			State: state,
		})
	})
}

func (cont *Controller) handleAllLogsTemplate(rw http.ResponseWriter, req *http.Request) {
	c := req.Context()
	cont.handleLogsTemplate(c, rw, "All", logging.GetRecentLogs(c))
}

func (cont *Controller) handleErrorLogsTemplate(rw http.ResponseWriter, req *http.Request) {
	c := req.Context()
	cont.handleLogsTemplate(c, rw, "Escalated", logging.GetRecentEscalatedLogs(c))
}

func (cont *Controller) handleLogsTemplate(c context.Context, rw http.ResponseWriter, name string, logs []zapcore.Entry) {
	type LogEntry struct {
		Time    time.Time
		Caller  string
		Level   string
		Message template.HTML
	}

	formatMessage := func(v string) template.HTML {
		v = html.EscapeString(v)
		v = strings.Replace(v, "\n", "<br>", -1)
		return template.HTML(v)
	}

	// Invert our logs, so we see recent ones first.
	entries := make([]LogEntry, len(logs))
	for i := range logs {
		e := &logs[len(logs)-i-1]
		entries[i] = LogEntry{
			Time:    e.Time,
			Caller:  e.Caller.TrimmedPath(),
			Level:   e.Level.String(),
			Message: formatMessage(e.Message),
		}
	}

	cont.site.RenderWithError(rw, "text/html", func() error {
		return cont.site.RenderTemplate(rw, "templates/logs.html", struct {
			Name string
			Logs []LogEntry
		}{
			Name: name,
			Logs: entries,
		})
	})
}

func (cont *Controller) handleStripSVG(rw http.ResponseWriter, req *http.Request) {
	c := req.Context()
	vars := mux.Vars(req)
	device := vars["device"]
	if device == "" {
		http.Error(rw, "missing 'device'", http.StatusBadRequest)
		return
	}

	strips, err := cont.Proxy.Strips(c, device)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		logging.S(c).Errorf("Could not get strip data for %q: %s", device, err)
		return
	}

	rw.Header().Set("Content-Type", "image/svg+xml")
	if err := RenderStripSVG(strips, rw); err != nil {
		http.Error(rw, "could not render SVG", http.StatusInternalServerError)
		return
	}
}
