
## Install basics

* vim
* tmux

### Notes on tmux

Configure your locale. Raspberry Pi apparently flashes to an invalid
configuration by default. Run this command and choose a locale. `en_US.UTF-8"`
is a fine value:

```
sudo dpkg-reconfigure locales
```

Create a new session:

```
tmux new-session -s name
```

List sessions:

```
tmux list-sessions
```

Attach session:

```
tmux attach-session -t name
```

## Setup Network

```
cd /etc/network/interfaces.d
sudo vim eth0
```

```
auto eth0
iface eth0 inet static
        address 192.168.1.211
        netmask 255.255.255.0
        gateway 192.168.1.1
        dns-nameservers 192.168.1.1
        dns-nameservers 8.8.8.8
        dns-nameservers 8.8.4.4
```

## Install Prometheus

sudo apt install prometheus

Edit `/etc/prometheus/prometheus.yml`:

```
	- job_name: pixelproxy
		static_configs:
    - targets: ['localhost:8080']
```

Enable by default:
```
sudo systemctl enable prometheus
```

And node:
```
sudo systemctl enable prometheus-node-exporter
```

## Install Grafana

For Raspberry Pi:

https://github.com/fg2it/grafana-on-raspberry/releases


Copy link to "arm64" `.deb` package.

Download the package (for bitness):

```
wget https://github.com/fg2it/grafana-on-raspberry/releases/download/v5.0.4/grafana_5.0.4_arm64.deb
```

Install it:

```
sudo dpkg -i ./grafana_5.0.4_arm64.deb
```

Enable by default:

```
sudo systemctl enable grafana-server
sudo systemctl start grafana-server
```

Login: http://<addr>:3000
Username: admin
Password: admin

Add data source:

* Type: Prometheus
* Address: http://localhost:9090

## Get filesystem working

Install btrfs programs:

```
sudo apt install btrfs-progs
```

Run `lsblk` to list block devices.

Unmount:

```
sudo mount

# If it's mounted...
sudo umount /media/pi/...
```

Format drive:
```
sudo mkfs.btrfs -L mylabel /dev/partition -f
```

Figure out UUID:

```
# Look for LABEL= on new disk.
sudo blkid
```

Configure auto mount: `/etc/systemd/system/mnt-pixelproxy_data.mount`

```
[Unit]
Description=PixelProxy Data

[Mount]
What=/dev/disk/by-uuid/4543bbad-c6b9-43af-945d-c3d0dd216595
Where=/mnt/pixelproxy_data
Type=btrfs
Options=defaults,compress

[Install]
WantedBy=multi-user.target
```

Enable by default:

```
sudo systemctl enable mnt-pixelproxy_data.mount
sudo systemctl start mnt-pixelproxy_data.mount
```

### Install compsize

`compsize` is a utility that shows the compressed size of files in `btrfs`. It
must be downloaded and built manually:

```
mkdir ~/src
cd ~/src
git clone https://github.com/kilobyte/compsize
cd ~/src/compsize
make
```

The utility can now be run at `~/src/compsize/compsize`.


## Install Go 1.10




## Configure Git overrides

(Follow installation instructions)

## Set persistent journal

Edit `/etc/systemd/journald.conf`:

```
# Storage=auto
Storage=persistent
```

Restart:

```
sudo systemctl restart systemd-journald
```

Let the Pi user use journalctl:

```
sudo usermod -a -G systemd-journal pi
```

## Install PixelProxy

### Create "pixelproxy" repository.

```
sudo mkdir /mnt/pixelproxy_data/storage
sudo chown pixelproxy:pixelproxy /mnt/pixelproxy_data/storage
```

### Install "systemctl" script.

```
# /etc/systemd/system/pixelproxy.service
#
# systemd service configuration file for PixelProxy.
# https://github.com/danjacques/pixelproxy/
#
# This service can be customized by providing an override
# file, via:
#
#     systemctl edit pixelproxy.service
#

[Unit]
Description=PixelProxy, a PixelPusher Proxy
Documentation=https://github.com/danjacques/pixelproxy/

# Start after the network has come online.
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
Restart=always
User=pixelproxy

ExecStart=/usr/bin/pixelproxy

# Wait 30 seconds after sending SIGTERM, then kill.
KillSignal=SIGTERM
TimeoutStopSec=30
SendSIGKILL=yes

[Install]
WantedBy=multi-user.target
```

### Configure "systemctl" override.

```
sudo systemctl edit pixelproxy.service
```

```
# Specialize PixelProxy invocation.

[Service]

ExecStart=
ExecStart=/usr/local/bin/pixelproxy \
	--verbose=info \
	--production=true \
	--interface=eth0 \
	--discovery_expiration=20s \
	--http_addr=0.0.0.0:8080 \
	--storage_path=/mnt/pixelproxy_data/storage \
	--storage_write_compression=NONE \
	--proxy_group_offset=5 \
	--enable_snapshot=true \
	--playback_auto_resume_delay=10s

[Unit]
RequiresMountsFor=/mnt/pixelproxy_data
```

### Enable via "systemctl"

```
sudo systemctl enable pixelproxy
```

### Allow it to restart system.

Edit: `/etc/sudoers.d/10-pixelproxy-shutdown`

```
# pixelproxy command must be able to reboot for system dashboard.
pixelproxy ALL=(ALL) NOPASSWD: /sbin/shutdown
```

Set ownership:
```
sudo chmod 0440 /etc/sudoers.d/10-pixelproxy-shutdown
```
