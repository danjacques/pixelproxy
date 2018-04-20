package fakepixelpusher

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/danjacques/pixelproxy/util"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/danjacques/gopushpixels/device"
	"github.com/danjacques/gopushpixels/discovery"
	"github.com/danjacques/gopushpixels/protocol"
	"github.com/danjacques/gopushpixels/protocol/pixelpusher"
	"github.com/danjacques/gopushpixels/support/bufferpool"
	"github.com/danjacques/gopushpixels/support/byteslicereader"
	"github.com/danjacques/gopushpixels/support/fmtutil"
	"github.com/danjacques/gopushpixels/support/network"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	app = util.Application{
		Verbosity:    zap.WarnLevel,
		Production:   false,
		ColorizeLogs: true,
	}

	config  = ""
	address = ""

	count           = 1
	discoveryPeriod = time.Second

	stripsAttached     = uint8(4)
	maxStripsPerPacket = uint8(1)
	pixelsPerStrip     = uint16(128)
)

func init() {
	pf := rootCmd.PersistentFlags()

	app.AddFlags(pf)

	pf.StringVarP(&config, "config", "c", config,
		"If specified, load device layout from a YAML at this path.")

	pf.StringVarP(&address, "address", "a", address,
		"If specified, the network address to instantiate on.")

	pf.DurationVar(&discoveryPeriod, "discovery_period", discoveryPeriod,
		"Period to broadcast discovery.")

	pf.Uint8Var(&stripsAttached, "strips_attached", stripsAttached,
		"Controls the number of strips attached.")

	pf.Uint16Var(&pixelsPerStrip, "pixels_per_strip", pixelsPerStrip,
		"Controls the number of LEDs per strip.")

	pf.Uint8Var(&maxStripsPerPacket, "max_strips_per_packet", maxStripsPerPacket,
		"Controls the number of strip data allowed per packet.")
}

var rootCmd = &cobra.Command{
	Use:   "fakepixelpusher",
	Short: "Application that operates as a fake PixelPusher device.",
	Long:  ``, // TODO: Fill in long descrpition.
	Run: func(cmd *cobra.Command, args []string) {
		app.Run(context.Background(), func(c context.Context) error {
			return rootCmdRun(c, cmd, args)
		})
	},
}

// Execute runs the application.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rootCmdRun(c context.Context, cmd *cobra.Command, args []string) error {
	// Create a discovery transmitter for this device.
	conn := discovery.DefaultTransmitterConn()
	ds := network.ResilientDatagramSender{
		Factory: conn.DatagramSender,
	}
	t := discovery.Transmitter{
		Logger: logging.S(c),
	}

	// Load our configuration.
	var cfg *Config
	if config != "" {
		var err error
		if cfg, err = LoadConfigYAML(config); err != nil {
			logging.S(c).Errorf("Failed to load config from %q: %s", config, err)
			return err
		}
	} else {
		cfg = &Config{
			Devices: []*Device{
				{},
			},
			DefaultDevice: &Device{
				Strips: stripsAttached,
				Pixels: pixelsPerStrip,
			},
		}
	}

	// Create our device headers.
	headers := make([]*protocol.DiscoveryHeaders, len(cfg.Devices))
	for i, d := range cfg.Devices {
		h, err := d.BuildHeaders(i, cfg)
		if err != nil {
			logging.S(c).Errorf("Failed to build headers for device #%d: %s", i, err)
			return err
		}

		headers[i] = h
	}

	// Create our devices.
	devices := make([]*device.Local, len(headers))
	for i, dh := range headers {
		addr, err := net.ResolveUDPAddr("udp4", address)
		if err != nil {
			logging.S(c).Errorf("Failed to resolve UDP address from %q: %s", address, err)
			return err
		}
		conn, err := net.ListenUDP("udp4", addr)
		if err != nil {
			logging.S(c).Errorf("Failed to open connection on %q: %s", addr, err)
			return err
		}

		d := device.Local{
			DeviceID: strconv.Itoa(i),
			Logger:   logging.S(c),
		}

		d.OnPacketData = func(buf *bufferpool.Buffer) {
			pr := d.DiscoveryHeaders().PixelPusher.PacketReader()

			var pkt pixelpusher.Packet
			if err := pr.ReadPacket(&byteslicereader.R{Buffer: buf.Bytes()}, &pkt); err != nil {
				logging.S(c).Warnf("Received invalid packet (%s) size %d on %q:\n%s",
					err, buf.Len(), d.String(), fmtutil.Hex(buf.Bytes()))
				return
			}

			logging.S(c).Infof("Received packet size %d on %q: %#v", buf.Len(), d.String(), pkt)
		}

		d.Start(conn)
		defer func() {
			if err := d.Close(); err != nil {
				logging.S(c).Warnf("Failed to close device: %s", err)
			}
		}()

		// Load our device headers.
		d.UpdateHeaders(dh)

		devices[i] = &d
		logging.S(c).Infof("Created local device #%d on %q:\n%s", i, d.Addr(), d.DiscoveryHeaders())
	}

	// Loop until we're cancelled, broadcasting our device.
	return util.LoopUntil(c, discoveryPeriod, func(c context.Context) error {
		for _, d := range devices {
			if err := t.Broadcast(&ds, d.DiscoveryHeaders()); err != nil {
				logging.S(c).Warnf("Failed to broadcast headers for %q: %s", d, err)
			}
		}
		return nil
	})
}
