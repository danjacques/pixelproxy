package profiling

import (
	"fmt"
	httpProf "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
)

// Profiler helps setup and manage profiling
type Profiler struct {
	// Dir, if set, is the path where profiling data will be written to.
	//
	// Can also be configured with "-profile-output-dir" flag.
	Dir string
	// ProfileCPU, if true, indicates that the profiler should profile the CPU.
	//
	// Requires Dir to be set, since it's where the profiler output is dumped.
	//
	// Can also be set with "-profile-cpu".
	ProfileCPU bool
	// ProfileHeap, if true, indicates that the profiler should profile heap
	// allocations.
	//
	// Requires Dir to be set, since it's where the profiler output is dumped.
	//
	// Can also be set with "-profile-heap".
	ProfileHeap bool

	// profilingCPU is true if 'Start' successfully launched CPU profiling.
	profilingCPU bool

	mu      sync.Mutex
	counter uint64
}

// AddFlags adds command line flags to common Profiler fields.
func (p *Profiler) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&p.Dir, "profile-output-dir", "",
		"If specified, allow generation of profiling artifacts, which will be written here.")
	fs.BoolVar(&p.ProfileCPU, "profile-cpu", false, "If specified, enables CPU profiling.")
	fs.BoolVar(&p.ProfileHeap, "profile-heap", false, "If specified, enables heap profiling.")
}

// Start starts the Profiler's configured operations.  On success, returns a
// function that can be called to shutdown the profiling server.
//
// Calling Stop is not necessary, but will enable end-of-operation profiling
// to be gathered.
func (p *Profiler) Start() error {
	if p.Dir == "" {
		if p.ProfileCPU {
			return errors.New("-profile-cpu requires -profile-output-dir to be set")
		}
		if p.ProfileHeap {
			return errors.New("-profile-heap requires -profile-output-dir to be set")
		}
	}
	if p.ProfileCPU {
		out, err := os.Create(p.generateOutPath("cpu"))
		if err != nil {
			return errors.Wrap(err, "failed to create CPU profile output file")
		}
		if err := pprof.StartCPUProfile(out); err != nil {
			return errors.Wrap(err, "start CPU profile")
		}
		p.profilingCPU = true
	}

	return nil
}

// AddHTTP adds HTTP proiling endpoints to the provided Router.
func (p *Profiler) AddHTTP(r *mux.Router) {
	// Register paths: https://golang.org/src/net/http/pprof/pprof.go
	r.HandleFunc("/debug/pprof", httpProf.Index).Methods("GET")
	r.HandleFunc("/debug/pprof/", httpProf.Index).Methods("GET")
	r.HandleFunc("/debug/pprof/cmdline", httpProf.Cmdline).Methods("GET")
	r.HandleFunc("/debug/pprof/profile", httpProf.Profile).Methods("GET")
	r.HandleFunc("/debug/pprof/symbol", httpProf.Symbol).Methods("GET")
	r.HandleFunc("/debug/pprof/trace", httpProf.Trace).Methods("GET")

	for _, p := range pprof.Profiles() {
		name := p.Name()
		r.Handle(fmt.Sprintf("/debug/%s", name), httpProf.Handler(name)).Methods("GET")
	}
}

// Stop stops the Profiler's operations.
func (p *Profiler) Stop() {
	if p.profilingCPU {
		pprof.StopCPUProfile()
		p.profilingCPU = false
	}

	// Take one final snapshot.
	_ = p.DumpSnapshot()
}

// DumpSnapshot dumps a profile snapshot to the configured output directory. If
// no output directory is configured, nothing will happen.
func (p *Profiler) DumpSnapshot() error {
	if p.Dir == "" {
		return nil
	}
	if p.ProfileHeap {
		if err := p.dumpHeapProfile(); err != nil {
			return errors.Wrap(err, "failed to dump heap profile")
		}
	}
	return nil
}
func (p *Profiler) dumpHeapProfile() (err error) {
	fd, err := os.Create(p.generateOutPath("memory"))
	if err != nil {
		return errors.Wrap(err, "failed to create output file")
	}
	defer func() {
		// If we could not close this file, propagate that error to the user.
		if cerr := fd.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// Get up-to-date statistics.
	runtime.GC()
	if err := pprof.WriteHeapProfile(fd); err != nil {
		return errors.Wrap(err, "failed to write heap profile")
	}
	return nil
}

func (p *Profiler) generateOutPath(base string) string {
	now := time.Now()
	counter := p.uniqueCounter()
	return filepath.Join(p.Dir, fmt.Sprintf("%s_%d_%d.prof", base, now.Unix(), counter))
}

func (p *Profiler) uniqueCounter() (v uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, p.counter = p.counter, p.counter+1
	return
}
