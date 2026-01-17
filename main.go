package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-crypto"
	host "github.com/libp2p/go-libp2p-host"
	inet "github.com/libp2p/go-libp2p-net"
	pstore "github.com/libp2p/go-libp2p-peerstore"
	protocol "github.com/libp2p/go-libp2p-protocol"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery"
	multiaddr "github.com/multiformats/go-multiaddr"
)

type discoveryNotifee struct {
	ctx      context.Context
	host     host.Host
	PeerChan chan pstore.PeerInfo
}

func (n *discoveryNotifee) HandlePeerFound(pi pstore.PeerInfo) {
	select {
	case n.PeerChan <- pi:
	case <-n.ctx.Done():
		return
	}
}

func initMDNS(ctx context.Context, peerhost host.Host, rendezvous string) chan pstore.PeerInfo {
	ser, err := discovery.NewMdnsService(ctx, peerhost, time.Second, rendezvous)
	if err != nil {
		panic(err)
	}

	n := &discoveryNotifee{
		ctx,
		peerhost,
		make(chan pstore.PeerInfo, 10),
	}

	ser.RegisterNotifee(n)
	return n.PeerChan
}

func getLocalIP() string {
	conn, err := net. Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		if localAddr.IP.String() != "" {
			return localAddr.IP.String()
		}
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}

	for _, iface := range interfaces {
		if iface.  Flags&net.FlagLoopback != 0 || iface. Flags&net. FlagUp == 0 {
			continue
		}

		addrs, err := iface.  Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.  IPNet)
			if !ok {
				continue
			}

			if ipNet.IP.To4() != nil {
				return ipNet.  IP.String()
			}
		}
	}

	fmt. Println("[WARNING] Could not detect network IP, falling back to localhost")
	return "127.0.0.1"
}

type StreamHandler struct {
	rw     *bufio.ReadWriter
	peerID string
	mu     sync. Mutex
}

var (
	stdReader        = bufio.NewReader(os. Stdin)
	streamMutex      sync.Mutex
	streams          = make([]*StreamHandler, 0)
	connectedPeerIDs = make(map[string]bool)
)

func handleStream(stream inet.Stream) {
	peerID := stream. Conn().RemotePeer().String()
	fmt.Printf("\n[INCOMING] New connection from peer: %s\n", peerID)

	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

	streamHandler := &StreamHandler{
		rw:     rw,
		peerID: peerID,
	}

	streamMutex.Lock()
	streams = append(streams, streamHandler)
	connectedPeerIDs[peerID] = true
	streamMutex.Unlock()

	printPeerCount()
	go readData(streamHandler)
}

func readData(sh *StreamHandler) {
	for {
		str, err := sh.rw. ReadString('\n')
		if err != nil {
			fmt. Printf("\n[DISCONNECTED] Peer %s disconnected\n", sh.peerID[: 12]+"...")

			// Remove from streams
			streamMutex.Lock()
			for i, stream := range streams {
				if stream.peerID == sh.peerID {
					streams = append(streams[:i], streams[i+1:]...)
					break
				}
			}
			delete(connectedPeerIDs, sh.peerID)
			streamMutex.Unlock()

			printPeerCount()
			return
		}

		if str == "" || str == "\n" {
			continue
		}

		fmt.Printf("\x1b[32m[PEER %s] %s\x1b[0m", sh.peerID[:12], str)
		fmt.Print("> ")
	}
}

func printPeerCount() {
	streamMutex.Lock()
	count := len(streams)
	streamMutex.Unlock()
	fmt.Printf("[INFO] Connected peers: %d\n", count)
}

func buildOpts(port int, priv crypto.PrivKey, ipAddress string) []libp2p.Option {
	sourceMultiAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", ipAddress, port))
	if err != nil {
		log. Fatal(err)
	}

	return []libp2p.Option{
		libp2p. Identity(priv),
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.EnableRelay(),
	}
}

func connectToPeer(ctx context.Context, peerhost host.Host, pid protocol.ID, peerAddr string) error {
	ma, err := multiaddr.NewMultiaddr(peerAddr)
	if err != nil {
		return fmt.Errorf("failed to parse multiaddr: %v", err)
	}

	peerInfo, err := pstore.InfoFromP2pAddr(ma)
	if err != nil {
		return fmt.Errorf("failed to extract peer info: %v", err)
	}

	// Check if already connected
	streamMutex.Lock()
	if connectedPeerIDs[peerInfo.ID. String()] {
		streamMutex.Unlock()
		return fmt.Errorf("already connected to this peer")
	}
	streamMutex.Unlock()

	peerhost.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, pstore.PermanentAddrTTL)

	fmt.Printf("[CONNECTING] Attempting to connect to %s.. .\n", peerInfo.ID.String()[:12]+"...")

	stream, err := peerhost.NewStream(ctx, peerInfo. ID, pid)
	if err != nil {
		return fmt. Errorf("failed to create stream: %v", err)
	}

	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
	streamHandler := &StreamHandler{
		rw:     rw,
		peerID: peerInfo.ID.String(),
	}

	streamMutex.Lock()
	streams = append(streams, streamHandler)
	connectedPeerIDs[peerInfo.ID.String()] = true
	streamMutex. Unlock()

	fmt.Printf("[SUCCESS] Connected to peer: %s\n", peerInfo.ID.String()[:12]+"...")
	printPeerCount()

	go readData(streamHandler)

	return nil
}

func broadcastMessage(message string) {
	streamMutex.Lock()
	streamList := make([]*StreamHandler, len(streams))
	copy(streamList, streams)
	streamMutex.Unlock()

	if len(streamList) == 0 {
		fmt.Println("[WARNING] No peers connected.  Message not sent.")
		return
	}

	for _, sh := range streamList {
		go func(handler *StreamHandler) {
			handler.mu.Lock()
			defer handler.mu.Unlock()

			_, err := handler.rw.WriteString(fmt.Sprintf("%s\n", message))
			if err != nil {
				fmt.Printf("[ERROR] Failed to send to peer:  %v\n", err)
				return
			}
			err = handler.rw.Flush()
			if err != nil {
				fmt.Printf("[ERROR] Failed to flush: %v\n", err)
				return
			}
		}(sh)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt. Println("Usage: ./chat-room <port> [topic]")
		fmt.Println("\nExamples:")
		fmt.Println("  ./chat-room 3000")
		fmt.Println("  ./chat-room 3000 gaming")
		fmt.Println("\nFeatures:")
		fmt.Println("  - Auto-discovery via mDNS (same WiFi)")
		fmt.Println("  - Manual connection via /connect")
		fmt.Println("  - Group chat (broadcast to all peers)")
		fmt.Println("  - Supports unlimited peers")
		os.Exit(0)
	}

	topic := "main"
	listenPort, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	if len(os.Args) >= 3 {
		topic = os.Args[2]
	}

	ipAddress := getLocalIP()
	fmt.Printf("[INFO] Auto-detected local IP: %s\n", ipAddress)

	ctx := context.Background()

	r := rand.Reader
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		log.Fatal(err)
	}

	peerhost, err := libp2p.New(ctx, buildOpts(listenPort, priv, ipAddress)...)
	if err != nil {
		log.Fatal(err)
	}

	pid := protocol.ID(fmt.Sprintf("/chat/1.1.0/%s", topic))
	peerhost.SetStreamHandler(pid, handleStream)

	fmt.Printf("[INFO] subscribed to topic: %s\n", topic)
	fmt.Printf("[INFO] listening on:  /ip4/%s/tcp/%v/p2p/%s\n", ipAddress, listenPort, peerhost.ID().Pretty())

	pchan := initMDNS(ctx, peerhost, "chat-room")

	go func() {
		for peer := range pchan {
			if peer.ID == peerhost.ID() {
				continue
			}

			streamMutex.Lock()
			if connectedPeerIDs[peer. ID.String()] {
				streamMutex.Unlock()
				continue
			}
			streamMutex.Unlock()

			fmt.Printf("[MDNS] Found peer: %s\n", peer.ID. String()[:12]+"...")
			peerhost.Peerstore().AddAddrs(peer.ID, peer.Addrs, pstore.PermanentAddrTTL)

			stream, err := peerhost.NewStream(ctx, peer.ID, pid)
			if err != nil {
				fmt.Printf("[ERROR] Error connecting to peer:  %v\n", err)
				continue
			}

			rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
			streamHandler := &StreamHandler{
				rw:     rw,
				peerID: peer.ID.String(),
			}

			streamMutex.Lock()
			streams = append(streams, streamHandler)
			connectedPeerIDs[peer.ID.String()] = true
			streamMutex. Unlock()

			fmt.Printf("[SUCCESS] Connected via mDNS to: %s\n", peer.ID.String()[:12]+"...")
			printPeerCount()

			go readData(streamHandler)
		}
	}()

	fmt.Println("\n[INFO] Waiting for peers via mDNS discovery...")
	fmt.Println("[INFO] Manual connection:  /connect /ip4/<ip>/tcp/<port>/p2p/<peer-id>")
	fmt.Println("[INFO] Commands: /help, /peers")
	fmt.Println("[INFO] Type messages to broadcast to all connected peers\n")

	go func() {
		for {
			fmt.Print("> ")
			input, err := stdReader.ReadString('\n')
			if err != nil {
				fmt. Println("[ERROR] Error reading input:", err)
				continue
			}

			trimmed := strings.TrimSpace(input)

			if strings.HasPrefix(trimmed, "/connect ") {
				peerAddr := strings.TrimPrefix(trimmed, "/connect ")
				if err := connectToPeer(ctx, peerhost, pid, peerAddr); err != nil {
					fmt.Printf("[ERROR] %v\n", err)
				}
			} else if trimmed == "/peers" || trimmed == "/help" {
				fmt.Println("\n[COMMANDS]")
				fmt.Println("  /peers                - Show connected peers")
				fmt.Println("  /connect <multiaddr>  - Connect to a peer manually")
				fmt.Println("  /help                 - Show this message")
				fmt.Println("  (regular text)        - Broadcast to all peers\n")
			} else if trimmed != "" {
				broadcastMessage(trimmed)
			}
		}
	}()

	select {}
}
