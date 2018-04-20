// Package pixelclient implements a client that sends packets to a PixelPusher
// device.
package pixelclient

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/danjacques/pixelproxy/applications/pixelproxy/web"
	"github.com/danjacques/pixelproxy/util"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/danjacques/gopushpixels/device"
	"github.com/danjacques/gopushpixels/pixel"
	"github.com/danjacques/gopushpixels/protocol"
	"github.com/danjacques/gopushpixels/protocol/pixelpusher"

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

	discoveryPeriod       = time.Minute
	loadPixelProxyProxies string
	devices               []string
	repeat                = time.Duration(0)

	maxStripsPerPacket = uint8(1)
	pixelsPerStrip     = uint16(128)
)

func init() {
	pf := rootCmd.PersistentFlags()

	app.AddFlags(pf)

	pf.DurationVar(&discoveryPeriod, "discovery_period", discoveryPeriod,
		"Period to broadcast discovery.")

	pf.StringVar(&loadPixelProxyProxies, "load_pixelproxy_proxies", loadPixelProxyProxies,
		"Load devices from a running PixelProxy instance by querying its API.")

	pf.StringSliceVarP(&devices, "device", "d", nil,
		"An [address:port] of a device to send to. Can be specified multiple times.")

	pf.DurationVar(&repeat, "repeat", repeat,
		"Repeat the command sequence every interval (default is once).")

	pf.Uint16Var(&pixelsPerStrip, "pixels_per_strip", pixelsPerStrip,
		"Controls the number of LEDs per strip.")

	pf.Uint8Var(&maxStripsPerPacket, "max_strips_per_packet", maxStripsPerPacket,
		"Controls the number of strip data allowed per packet.")
}

var rootCmd = &cobra.Command{
	Use:   "pixelclient commands...",
	Short: "Testing PixelPusher JSON command sending client.",
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
	const playbackRoundErrorDuration = time.Second

	// Resolve our strings to UDP addresses.
	addrs := make([]*net.UDPAddr, len(devices))
	for i, arg := range devices {
		var err error
		if addrs[i], err = net.ResolveUDPAddr("udp4", arg); err != nil {
			logging.S(c).Errorf("Invalid UDP address: %s", arg)
			return err
		}
	}

	// Read in packet JSON and store them as commands.
	packets := make([]*protocol.Packet, len(args))
	for i, arg := range args {
		pkt := &protocol.Packet{
			PixelPusher: &pixelpusher.Packet{},
		}

		if err := parsePacketJSON(arg, pkt.PixelPusher); err != nil {
			logging.S(c).Errorf("Could not parse command #%d from:\n%s", i, arg)
			return err
		}

		packets[i] = pkt
	}

	for {
		switch err := beginPlaybackRound(c, addrs, packets); errors.Cause(err) {
		case nil, context.Canceled:
			return nil

		default:
			logging.S(c).Errorf("Failed playback round, sleeping for %s and retrying...", playbackRoundErrorDuration)
			time.Sleep(playbackRoundErrorDuration)
		}
	}
}

func beginPlaybackRound(c context.Context, addrs []*net.UDPAddr, packets []*protocol.Packet) error {
	if loadPixelProxyProxies != "" {
		ppAddrs, err := loadPixelProxyDeviceAddrs(c, loadPixelProxyProxies)
		if err != nil {
			logging.S(c).Errorf("Could not load PixelProxy devices from %q API: %s", loadPixelProxyProxies, err)
			return err
		}

		if len(ppAddrs) == 0 {
			logging.S(c).Warnf("PixelProxy did not export any devices; trying again later...")
			return errors.New("no devices")
		}

		logging.S(c).Infof("Loaded %d device(s) from API %s!", len(ppAddrs), loadPixelProxyProxies)
		addrs = append(append([]*net.UDPAddr(nil), addrs...), ppAddrs...)
	}

	var reg device.Registry
	router := device.Router{
		Registry: &reg,
		Logger:   logging.S(c),
	}

	// Generate stubs for these arguments.
	stubs := make([]*device.Remote, len(addrs))
	for i, addr := range addrs {
		i := i

		// Specialize the stub.
		dh := protocol.DiscoveryHeaders{
			DeviceHeader: protocol.DeviceHeader{
				DeviceType: protocol.PixelPusherDeviceType,
			},
			PixelPusher: &pixelpusher.Device{},
		}
		dh.SetIP4Address(addr.IP)
		dh.PixelPusher.MyPort = uint16(addr.Port)
		dh.PixelPusher.MaxStripsPerPacket = maxStripsPerPacket
		dh.PixelPusher.PixelsPerStrip = pixelsPerStrip

		stub := device.MakeRemote(fmt.Sprintf("Stub %d", i), &dh)
		defer stub.MarkDone()

		reg.Add(stub)
		stubs[i] = stub
	}

	for {
		logging.S(c).Debugf("Sending %d command(s) to %d device(s)...", len(packets), len(stubs))

		// Iterate through each packet. Here, pkt is a shallow copy of the Packet,
		// which is good b/c we're going to fill in its ID.
		for _, pkt := range packets {
			if err := dispatchPacket(c, pkt, &router, stubs); err != nil {
				return err
			}
		}

		if repeat <= 0 {
			break
		}
		logging.S(c).Debugf("Sleeping %s and repeating...", repeat)
		if err := util.Sleep(c, repeat); err != nil {
			return err
		}
	}

	return nil
}

func dispatchPacket(c context.Context, pkt *protocol.Packet, r *device.Router, devices []*device.Remote) error {

	doneC := make(chan error)
	for _, d := range devices {
		d := d

		go func() {
			doneC <- func() error {
				logging.S(c).Debugf("Dispatching packet %#v to %s @ %s", pkt, d, d.Addr())

				// Write the packet and record its result.
				if err := r.Route(device.InvalidOrdinal(), d.ID(), pkt); err != nil {
					logging.S(c).Warnf("Could not route packet to %s: %s", d, err)
					return err
				}
				return nil
			}()
		}()
	}

	// Wait for all packets to be sent.
	wasError := false
	for range devices {
		if err := <-doneC; err != nil {
			wasError = true
		}
	}
	close(doneC)

	if wasError {
		return errors.New("failed to dispatch packets")
	}
	return nil
}

func parsePacketJSON(arg string, pkt *pixelpusher.Packet) error {
	data := []byte(arg)

	insn := struct {
		Command string `json:"command"`
	}{}
	if err := json.Unmarshal(data, &insn); err != nil {
		return err
	}

	switch insn.Command {
	case "reset":
		return parseCommandJSON(pkt, data, &pixelpusher.ResetCommand{})
	case "global_brightness_set":
		return parseCommandJSON(pkt, data, &pixelpusher.GlobalBrightnessSetCommand{})
	case "strip_brightness_set":
		return parseCommandJSON(pkt, data, &pixelpusher.StripBrightnessSetCommand{})
	case "wifi_configure":
		return parseCommandJSON(pkt, data, &pixelpusher.WiFiConfigureCommand{})
	case "led_configure":
		return parseCommandJSON(pkt, data, &pixelpusher.LEDConfigureCommand{})
	case "pixels":
		return parsePixelsJSON(pkt, data)
	default:
		return errors.Errorf("unknown 'command' value: %q", insn.Command)
	}
}

func parseCommandJSON(pkt *pixelpusher.Packet, data []byte, cmdBase pixelpusher.Command) error {
	if err := json.Unmarshal(data, cmdBase); err != nil {
		return err
	}
	pkt.Command = cmdBase
	return nil
}

func parsePixelsJSON(pkt *pixelpusher.Packet, data []byte) error {
	var insn struct {
		Strips  []int  `json:"strips"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(data, &insn); err != nil {
		return err
	}

	var getPixelValue func(i int) pixel.P

	// Shorthand generator for a getPixelValue function that returns a single
	// pixel value.
	singlePixel := func(p pixel.P) func(int) pixel.P {
		return func(_ int) pixel.P {
			return p
		}
	}

	switch insn.Pattern {
	case "red":
		getPixelValue = singlePixel(pixel.P{Red: 0xFF})
	case "green":
		getPixelValue = singlePixel(pixel.P{Green: 0xFF})
	case "blue":
		getPixelValue = singlePixel(pixel.P{Blue: 0xFF})
	case "random":
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		getPixelValue = func(i int) pixel.P {
			v := r.Uint32()
			return pixel.P{
				Red:   byte(v & 0xFF),
				Green: byte((v >> 8) & 0xFF),
				Blue:  byte((v >> 16) & 0xFF),
			}
		}
	default:
		return errors.Errorf("unknown pattern %q", insn.Pattern)
	}

	pkt.StripStates = make([]*pixelpusher.StripState, len(insn.Strips))
	for i := range pkt.StripStates {
		ss := pixelpusher.StripState{
			StripNumber: pixelpusher.StripNumber(insn.Strips[i]),
		}
		ss.Pixels.Reset(int(pixelsPerStrip))
		for i := 0; i < ss.Pixels.Len(); i++ {
			ss.Pixels.SetPixel(i, getPixelValue(i))
		}

		pkt.StripStates[i] = &ss
	}
	return nil
}

func loadPixelProxyDeviceAddrs(c context.Context, pp string) ([]*net.UDPAddr, error) {
	// Load JSON status API endpoint.
	url := pp + "/_api/status"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(c)

	logging.S(c).Infof("Loading PixelProxy proxy devices from: %s", url)
	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		logging.S(c).Errorf("Could not load PixelProxy devices from %s: %s", url, err)
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Parse the response body as JSON.
	var ws web.Status
	r := json.NewDecoder(resp.Body)
	if err := r.Decode(&ws); err != nil {
		logging.S(c).Errorf("Failed to decode JSON response: %s", err)
		return nil, err
	}

	addrs := make([]*net.UDPAddr, 0, len(ws.Devices))
	for _, d := range ws.Devices {
		if d.Type != "proxy" {
			continue
		}

		addr, err := net.ResolveUDPAddr("udp4", d.Address)
		if err != nil {
			logging.S(c).Warnf("Failed to parse UDP address from %q: %s", d, err)
			continue
		}
		addrs = append(addrs, addr)
	}

	return addrs, nil
}
