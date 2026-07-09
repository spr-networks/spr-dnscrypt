package main

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var DNSCryptBin = "/dnscrypt-proxy"

// gDNSCryptVersion is stamped at image build time via
// -ldflags "-X main.gDNSCryptVersion=<version>".
var gDNSCryptVersion = "unknown"

type ResolverHealth struct {
	Name     string
	Protocol string
	RTTms    int
}

type Daemon struct {
	mtx         sync.Mutex
	cmd         *exec.Cmd
	done        chan struct{}
	gen         int
	running     bool
	ready       bool
	stopping    bool
	startedAt   time.Time
	liveServers int
	fastest     string
	lastError   string
	listenAddrs []string
	resolvers   map[string]ResolverHealth
	backoff     time.Duration
}

type DaemonStatus struct {
	Running       bool
	Ready         bool
	Version       string
	UptimeSeconds int64
	Uptime        string
	ListenAddrs   []string
	LiveServers   int
	FastestServer string
	Resolvers     []ResolverHealth
	LastError     string
}

func NewDaemon() *Daemon {
	return &Daemon{
		resolvers: map[string]ResolverHealth{},
		backoff:   2 * time.Second,
	}
}

// listenAddresses returns the addresses dnscrypt-proxy binds inside the
// container: the container IP on the plugin's docker bridge (reached by SPR's
// dns service), plus localhost for in-container debugging. Never a host port.
func listenAddresses() []string {
	addrs := []string{"127.0.0.1:53"}
	if ip := containerIP(); ip != "" {
		addrs = append([]string{net.JoinHostPort(ip, "53")}, addrs...)
	}
	return addrs
}

func containerIP() string {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func (d *Daemon) Start() error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	return d.startLocked()
}

func (d *Daemon) startLocked() error {
	if d.running {
		return nil
	}
	d.stopping = false

	addrs := listenAddresses()
	if err := writeTOML(addrs); err != nil {
		d.lastError = "failed to write dnscrypt-proxy.toml: " + err.Error()
		return fmt.Errorf("%s", d.lastError)
	}

	cmd := exec.Command(DNSCryptBin, "-config", TomlFile)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // dlog writes to stderr; merge the streams

	if err := cmd.Start(); err != nil {
		d.lastError = "failed to start dnscrypt-proxy: " + err.Error()
		return err
	}

	d.gen++
	gen := d.gen
	d.cmd = cmd
	d.done = make(chan struct{})
	done := d.done
	d.running = true
	d.ready = false
	d.startedAt = time.Now()
	d.liveServers = 0
	d.fastest = ""
	d.lastError = ""
	d.resolvers = map[string]ResolverHealth{}
	d.listenAddrs = addrs

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println("[dnscrypt-proxy]", line)
			d.parseLine(gen, line)
		}
	}()

	go func() {
		err := cmd.Wait()
		d.mtx.Lock()
		if gen != d.gen {
			d.mtx.Unlock()
			return
		}
		d.running = false
		d.ready = false
		close(done)
		intentional := d.stopping
		if !intentional {
			if err != nil {
				d.lastError = "dnscrypt-proxy exited: " + err.Error()
			} else {
				d.lastError = "dnscrypt-proxy exited unexpectedly"
			}
			fmt.Println("[-]", d.lastError)
			// auto-restart with backoff; reset backoff after a stable hour
			if time.Since(d.startedAt) > time.Hour {
				d.backoff = 2 * time.Second
			}
			delay := d.backoff
			if d.backoff < time.Minute {
				d.backoff *= 2
			}
			d.mtx.Unlock()
			time.Sleep(delay)
			d.mtx.Lock()
			if !d.running && !d.stopping {
				if err := d.startLocked(); err != nil {
					fmt.Println("[-] restart failed:", err)
				}
			}
			d.mtx.Unlock()
			return
		}
		d.mtx.Unlock()
	}()

	fmt.Println("[+] dnscrypt-proxy started, listening on", strings.Join(addrs, ", "))
	return nil
}

var reReady = regexp.MustCompile(`dnscrypt-proxy is ready - live servers: (\d+)`)
var reServerOK = regexp.MustCompile(`\[([A-Za-z0-9._+-]+)\] OK \(([^)]+)\)(?:.*rtt: (\d+)ms)?`)
var reFastest = regexp.MustCompile(`Server with the lowest initial latency: ([A-Za-z0-9._+-]+)`)
var reError = regexp.MustCompile(`\[(ERROR|CRITICAL|FATAL)\] (.*)`)

func (d *Daemon) parseLine(gen int, line string) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if gen != d.gen {
		return
	}
	if m := reReady.FindStringSubmatch(line); m != nil {
		d.ready = true
		d.liveServers, _ = strconv.Atoi(m[1])
		return
	}
	if m := reServerOK.FindStringSubmatch(line); m != nil {
		rtt := -1
		if m[3] != "" {
			rtt, _ = strconv.Atoi(m[3])
		}
		d.resolvers[m[1]] = ResolverHealth{Name: m[1], Protocol: m[2], RTTms: rtt}
		return
	}
	if m := reFastest.FindStringSubmatch(line); m != nil {
		d.fastest = m[1]
		return
	}
	if m := reError.FindStringSubmatch(line); m != nil {
		d.lastError = m[2]
		return
	}
}

// Stop terminates the child (SIGTERM, then SIGKILL after a grace period).
func (d *Daemon) Stop() {
	d.mtx.Lock()
	if !d.running || d.cmd == nil {
		d.mtx.Unlock()
		return
	}
	d.stopping = true
	cmd := d.cmd
	done := d.done
	d.mtx.Unlock()

	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

func (d *Daemon) Restart() error {
	d.Stop()
	return d.Start()
}

func (d *Daemon) Status() DaemonStatus {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	st := DaemonStatus{
		Running:       d.running,
		Ready:         d.ready,
		Version:       gDNSCryptVersion,
		ListenAddrs:   append([]string{}, d.listenAddrs...),
		LiveServers:   d.liveServers,
		FastestServer: d.fastest,
		LastError:     d.lastError,
		Resolvers:     []ResolverHealth{},
	}
	if d.running {
		up := time.Since(d.startedAt)
		st.UptimeSeconds = int64(up.Seconds())
		st.Uptime = up.Truncate(time.Second).String()
	}
	for _, r := range d.resolvers {
		st.Resolvers = append(st.Resolvers, r)
	}
	// fastest first; resolvers with unknown rtt (-1) last; ties by name
	sort.Slice(st.Resolvers, func(i, j int) bool {
		a, b := st.Resolvers[i], st.Resolvers[j]
		if (a.RTTms < 0) != (b.RTTms < 0) {
			return b.RTTms < 0
		}
		if a.RTTms != b.RTTms {
			return a.RTTms < b.RTTms
		}
		return a.Name < b.Name
	})
	return st
}
