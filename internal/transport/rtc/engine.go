package rtc

import (
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
)

type Config struct {
	STUNServers []string
	UDPPort     int
	HostIPs     []string
}

var virtualPrefixes = []string{"docker", "br-", "veth", "virbr", "vmnet", "cni"}

var (
	engineMu   sync.RWMutex
	iceServers = []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}
	api     *webrtc.API
	mux     *net.UDPConn
	hostIPs map[string]bool
)

func Configure(cfg Config) error {
	engineMu.Lock()
	defer engineMu.Unlock()

	if len(cfg.STUNServers) > 0 {
		servers := make([]webrtc.ICEServer, 0, len(cfg.STUNServers))
		for _, u := range cfg.STUNServers {
			if u = strings.TrimSpace(u); u != "" {
				servers = append(servers, webrtc.ICEServer{URLs: []string{u}})
			}
		}
		if len(servers) > 0 {
			iceServers = servers
		}
	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return err
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		return err
	}

	hostIPs = nil
	if len(cfg.HostIPs) > 0 {
		hostIPs = make(map[string]bool, len(cfg.HostIPs))
		for _, ip := range cfg.HostIPs {
			if ip = strings.TrimSpace(ip); ip != "" {
				hostIPs[ip] = true
			}
		}
		slog.Info("rtc: advertising only these addresses", "ips", cfg.HostIPs)
	}

	se := webrtc.SettingEngine{LoggerFactory: slogFactory{}}
	se.SetInterfaceFilter(usableInterface)
	se.SetIPFilter(usableIP)

	if cfg.UDPPort > 0 {
		if mux != nil {
			_ = mux.Close()
		}
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: cfg.UDPPort})
		if err != nil {
			return err
		}
		if err := conn.SetReadBuffer(4 << 20); err != nil {
			slog.Warn("rtc: could not grow the read buffer", "error", err)
		}
		if err := conn.SetWriteBuffer(4 << 20); err != nil {
			slog.Warn("rtc: could not grow the write buffer", "error", err)
		}
		mux = conn
		se.SetICEUDPMux(webrtc.NewICEUDPMux(slogFactory{}.NewLogger("ice-udp-mux"), conn))
	}

	slog.Info("rtc: configured", "udp_port", cfg.UDPPort, "ice_servers", len(iceServers), "host_ips", cfg.HostIPs)
	api = webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(ir),
		webrtc.WithSettingEngine(se),
	)
	return nil
}

func usableInterface(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			slog.Debug("rtc: interface skipped", "interface", name)
			return false
		}
	}
	return true
}

func usableIP(ip net.IP) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if len(hostIPs) > 0 {
		return hostIPs[ip.String()]
	}
	return true
}

func newPeerConnection() (*webrtc.PeerConnection, error) {
	engineMu.RLock()
	a, servers := api, iceServers
	engineMu.RUnlock()

	cfg := webrtc.Configuration{ICEServers: servers}
	if a == nil {
		return webrtc.NewPeerConnection(cfg)
	}
	return a.NewPeerConnection(cfg)
}
