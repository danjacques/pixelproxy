package pixelcat

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/danjacques/gopushpixels/replay/streamfile"
	"github.com/danjacques/pixelproxy/util"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	app = util.Application{
		Verbosity:    zap.WarnLevel,
		Production:   false,
		ColorizeLogs: true,
	}

	alwaysDumpHex = false
)

func init() {
	pf := rootCmd.PersistentFlags()

	app.AddFlags(pf)

	pf.BoolVarP(&alwaysDumpHex, "always_dump_hex", "d", alwaysDumpHex,
		"Always dump hex content of packets.")
}

var rootCmd = &cobra.Command{
	Use:   "pixelcat",
	Short: "Dump the contents of a PixelProxy save file",
	Long:  ``, // TODO: Fill in long descrpition.
	Run: func(cmd *cobra.Command, args []string) {
		app.Run(context.Background(), func(c context.Context) error {
			return rootCmdRun(c, cmd, args)
		})
	},
}

// Execute runs the main application.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rootCmdRun(c context.Context, cmd *cobra.Command, args []string) error {
	for _, arg := range args {
		if err := dumpFile(c, arg, os.Stdout); err != nil {
			logging.S(c).Errorf("Error dumping file %q: %s", arg, err)
			return err
		}
	}
	return nil
}

func dumpFile(c context.Context, path string, out io.Writer) (err error) {
	sr, err := streamfile.MakeEventStreamReader(path)
	if err != nil {
		return errors.Wrap(err, "opening file")
	}
	defer func() {
		if err := sr.Close(); err != nil {
			logging.S(c).Warnf("Failed to close stream file %q: %s", path, err)
		}
	}()

	// We will panic on error, and translate that panic into an error result.
	defer func() {
		if mustErr, ok := recover().(error); ok {
			logging.S(c).Errorf("Panic during dump: %s", mustErr)
			err = mustErr
		}
	}()
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	mustWrite := func(_ interface{}, err error) {
		must(err)
	}

	mustDumpHex := func(v []byte) {
		wc := hex.Dumper(out)
		mustWrite(io.Copy(wc, bytes.NewReader(v)))
		must(wc.Close())
		mustWrite(fmt.Fprintln(out))
	}

	md := sr.Metadata()
	mustWrite(fmt.Fprintln(out, "\n\nRead file metadata:"))
	must(proto.MarshalText(out, md))
	mustWrite(fmt.Fprint(out, "\n\n"))

	for index := 0; ; index++ {
		e, err := sr.ReadEvent()
		if err != nil {
			if err == io.EOF {
				logging.S(c).Debugf("Encountered EOF.")
				return nil
			}
			return errors.Wrap(err, "reading events from file")
		}

		var offset time.Duration
		if v := e.Offset; v != nil {
			var err error
			if offset, err = ptypes.Duration(v); err != nil {
				logging.S(c).Warnf("Failed to decode offset from event #%d: %s", index, err)
				offset = 0
			}
		}
		mustWrite(fmt.Fprintf(out, "Packet #%d at offset %s:", index, offset))

		if pkt := e.GetPacket(); pkt != nil {
			device := sr.ResolveDeviceForIndex(pkt.Device)
			if device == nil {
				mustWrite(fmt.Fprintf(out, "  Packet references out-of-range device %d:\n%s", pkt.Device, pkt))
				continue
			}

			decoded, err := pkt.Decode(device)
			if err != nil {
				mustWrite(fmt.Fprintf(out, "  Failed to decode event: %s\n%s", err, pkt))
				continue
			}

			switch {
			case decoded.PixelPusher != nil:
				pp := decoded.PixelPusher

				for _, ss := range pp.StripStates {
					mustWrite(fmt.Fprintf(out, "  Strip state for strip %d:\n", ss.StripNumber))
					mustDumpHex(ss.Pixels.Bytes())
				}
			}
		}
	}
}
