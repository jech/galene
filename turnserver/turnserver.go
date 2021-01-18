package turnserver

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net"
	"strconv"

	"github.com/pion/turn/v2"
	"github.com/pion/webrtc/v3"
)

var username string
var password string
var server *turn.Server
var Address string
var addresses []net.Addr

func publicAddresses() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var as []net.IP

	for _, addr := range addrs {
		switch addr := addr.(type) {
		case *net.IPNet:
			a := addr.IP.To4()
			if a == nil {
				continue
			}
			if !a.IsGlobalUnicast() {
				continue
			}
			if a[0] == 10 ||
				a[0] == 172 && a[1] >= 16 && a[1] < 32 ||
				a[0] == 192 && a[1] == 168 {
				continue
			}
			as = append(as, a)
		}
	}
	return as, nil
}

func listener(a net.IP, port int, relay net.IP) (*turn.PacketConnConfig, *turn.ListenerConfig) {
	var pcc *turn.PacketConnConfig
	var lc *turn.ListenerConfig
	s := net.JoinHostPort(a.String(), strconv.Itoa(port))

	p, err := net.ListenPacket("udp4", s)
	if err == nil {
		pcc = &turn.PacketConnConfig{
			PacketConn: p,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: relay,
				Address:      a.String(),
			},
		}
	} else {
		log.Printf("TURN: listenPacket(%v): %v", s, err)
	}

	l, err := net.Listen("tcp4", s)
	if err == nil {
		lc = &turn.ListenerConfig{
			Listener: l,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: relay,
				Address:      a.String(),
			},
		}
	} else {
		log.Printf("TURN: listen(%v): %v", s, err)
	}

	return pcc, lc
}

func Start() error {
	if server != nil {
		return errors.New("TURN server already started")
	}

	if Address == "" {
		return errors.New("built-in TURN server disabled")
	}
	addr, err := net.ResolveUDPAddr("udp4", Address)
	if err != nil {
		return err
	}

	username = "galene"
	buf := make([]byte, 6)
	_, err = rand.Read(buf)
	if err != nil {
		return err
	}

	buf2 := make([]byte, 8)
	base64.RawStdEncoding.Encode(buf2, buf)
	password = string(buf2)

	var lcs []turn.ListenerConfig
	var pccs []turn.PacketConnConfig

	if addr.IP != nil && !addr.IP.IsUnspecified() {
		a := addr.IP.To4()
		if a == nil {
			return errors.New("couldn't parse address")
		}
		pcc, lc := listener(net.IP{0, 0, 0, 0}, addr.Port, a)
		if pcc != nil {
			pccs = append(pccs, *pcc)
			addresses = append(addresses, &net.UDPAddr{
				IP:   a,
				Port: addr.Port,
			})
		}
		if lc != nil {
			lcs = append(lcs, *lc)
			addresses = append(addresses, &net.TCPAddr{
				IP:   a,
				Port: addr.Port,
			})
		}
	} else {
		as, err := publicAddresses()
		if err != nil {
			return err
		}

		if len(as) == 0 {
			return errors.New("no public addresses")
		}

		for _, a := range as {
			pcc, lc := listener(a, addr.Port, a)
			if pcc != nil {
				pccs = append(pccs, *pcc)
				addresses = append(addresses, &net.UDPAddr{
					IP:   a,
					Port: addr.Port,
				})
			}
			if lc != nil {
				lcs = append(lcs, *lc)
				addresses = append(addresses, &net.TCPAddr{
					IP:   a,
					Port: addr.Port,
				})
			}
		}
	}

	if len(pccs) == 0 && len(lcs) == 0 {
		return errors.New("couldn't establish any listeners")
	}

	server, err = turn.NewServer(turn.ServerConfig{
		Realm: "galene.org",
		AuthHandler: func(u, r string, src net.Addr) ([]byte, bool) {
			if u != username || r != "galene.org" {
				return nil, false
			}
			return turn.GenerateAuthKey(u, r, password), true
		},
		ListenerConfigs:   lcs,
		PacketConnConfigs: pccs,
	})

	if err != nil {
		addresses = nil
		return err
	}

	return nil
}

func ICEServers() []webrtc.ICEServer {
	if len(addresses) == 0 {
		return nil
	}

	var urls []string
	for _, a := range addresses {
		switch a := a.(type) {
		case *net.UDPAddr:
			urls = append(urls, "turn:"+a.String())
		case *net.TCPAddr:
			urls = append(urls, "turn:"+a.String()+"?transport=tcp")
		default:
			log.Printf("unexpected TURN address %T", a)
		}
	}

	return []webrtc.ICEServer{
		{
			URLs:       urls,
			Username:   username,
			Credential: password,
		},
	}

}

func Stop() {
	addresses = nil
	if server == nil {
		return
	}
	server.Close()
	server = nil
	return
}
