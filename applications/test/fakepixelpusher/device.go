package fakepixelpusher

import (
	"bufio"
	"net"
	"os"

	"github.com/danjacques/gopushpixels/protocol"
	"github.com/danjacques/gopushpixels/protocol/pixelpusher"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// Config is as top-level configuration file.
type Config struct {
	// Devices is a list of Devices to populate.
	Devices []*Device `yaml:"devices"`

	// DefaultDevice is a configured default device.
	DefaultDevice *Device `yaml:"default_device"`
}

// LoadConfigYAML loads a Config from a YAML file.
func LoadConfigYAML(path string) (*Config, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "could not open path %q", path)
	}
	defer func() {
		_ = fd.Close()
	}()

	// Use a buffered reader.
	br := bufio.NewReader(fd)

	var config Config
	dec := yaml.NewDecoder(br)
	dec.SetStrict(true)
	if err := dec.Decode(&config); err != nil {
		return nil, errors.Wrap(err, "failed to decode config")
	}

	return &config, nil
}

// Device is a configuration for a single device.
type Device struct {
	// HardwareAddress is the hardware address, in text format.
	//
	// For example: 00:01:02:03:04:05
	HardwareAddress string `yaml:"hardware_address,omitempty"`

	// Group is the group ordinal value.
	Group int32 `yaml:"group,omitempty"`
	// Controller is the controller ordinal.
	Controller int32 `yaml:"controller,omitempty"`

	// Strips is the number of strips.
	Strips uint8 `yaml:"strips,omitempty"`
	// Pixels is the number of pixels per strip.
	Pixels uint16 `yaml:"pixels,omitempty"`
}

// BuildHeaders builds DiscoveryHeaders for this Device.
//
// If fields are missing, the values from def will be used. Generated fields
// that need to be unique will use index to differentiate themselves.
func (d *Device) BuildHeaders(index int, cfg *Config) (*protocol.DiscoveryHeaders, error) {
	def := cfg.DefaultDevice

	// Parse/generate our address.
	addr, err := buildAddress(d, def, index)
	if err != nil {
		return nil, err
	}
	addrArray := [6]byte{}
	copy(addrArray[:6], addr)

	// Determine # of pixels.
	if def != nil {
		if d.Pixels == 0 {
			d.Pixels = def.Pixels
		}

		// Determine # of strips.
		if d.Strips == 0 {
			d.Strips = def.Strips
		}

		// Determine our group value.
		if d.Group <= 0 {
			d.Group = def.Group
		}

		if d.Controller <= 0 {
			d.Controller = def.Controller
		}
	}

	// If we have a zero-value controller, auto-increment with index.
	if d.Controller <= 0 {
		d.Controller = int32(index)
	}

	dh := protocol.DiscoveryHeaders{
		DeviceHeader: protocol.DeviceHeader{
			MacAddress:       addrArray,
			IPAddress:        [4]byte{0, 0, 0, 0}, // Will be filled in by device.
			DeviceType:       protocol.PixelPusherDeviceType,
			ProtocolVersion:  protocol.DefaultProtocolVersion,
			VendorID:         0x1234,
			ProductID:        0xABCD,
			HardwareRevision: 1,
			SoftwareRevision: pixelpusher.MinAcceptableSoftwareRevision,
		},
		PixelPusher: &pixelpusher.Device{
			DeviceHeader: pixelpusher.DeviceHeader{
				StripsAttached:     d.Strips,
				MaxStripsPerPacket: maxStripsPerPacket,
				PixelsPerStrip:     d.Pixels,
				UpdatePeriod:       100,
				DeltaSequence:      0,
				ControllerOrdinal:  d.Controller,
				GroupOrdinal:       d.Group,
				ArtNetUniverse:     0,
				ArtNetChannel:      0,
			},
			DeviceHeaderExt101: pixelpusher.DeviceHeaderExt101{
				MyPort: 0, // Will be filled in by device.
			},
		},
	}
	dh.PixelPusher.StripFlags = make([]pixelpusher.StripFlags, dh.PixelPusher.StripsAttached)
	return &dh, nil
}

func buildAddress(d, def *Device, index int) (net.HardwareAddr, error) {
	const defaultAddress = "DE:FA:17:00:00:00"

	addr, offset := d.HardwareAddress, false
	if addr == "" {
		// Use a default address value.
		offset = true
		if def != nil {
			addr = def.HardwareAddress
		}
		if addr == "" {
			addr = defaultAddress
		}
	}

	parsedAddr, err := net.ParseMAC(addr)
	if err != nil {
		return nil, errors.Wrapf(err, "could not parse MAC from %q", addr)
	}

	if len(parsedAddr) != 6 {
		return nil, errors.Errorf("parsed address from %q (%s) has length %d",
			addr, parsedAddr, len(parsedAddr))
	}
	if offset {
		parsedAddr[5] = byte(index)
	}
	return parsedAddr, nil
}
