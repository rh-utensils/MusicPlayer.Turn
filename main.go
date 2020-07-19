package main

import (
	"flag"
	"fmt"
	"strings"
	"log"
	"net"
	"os"
	"os/signal"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"syscall"

	"github.com/pion/turn/v2"
)

func getPublicIP() (string) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		log.Panicf("Failed to get public IP", err)
	}
	defer resp.Body.Close()

	ipBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Panicf("Failed to parse public IP from body", err)
	}

	ip := fmt.Sprintf("%s", ipBytes)

	publicIP := ip
    if strings.Count(ip, "%s") == 1 {
        publicIP = fmt.Sprintf(ip)
    }

	return publicIP
}

func getPort() (int) {
	portString := os.Getenv("PORT");
	if (portString == "") {
		portString = "3478"
	}

	port, err := strconv.Atoi(portString)
	if err != nil {
		log.Panicf("Failed to get heroku Port", err)
	}

	return port;
}

func main() {
	publicIP := flag.String("public-ip", getPublicIP(), "IP Address that TURN can be contacted by.")
	port := flag.Int("port", getPort(), "Listening port.")
	users := flag.String("users", "musicplayer=z!ErcBpHfgV%QA5Bz*a6", "List of username and password (e.g. \"user=pass,user=pass\")")
	realm := flag.String("realm", "pion.ly", "Realm (defaults to \"pion.ly\")")
	flag.Parse()

	if len(*publicIP) == 0 {
		log.Fatalf("'public-ip' is required")
	} else if len(*users) == 0 {
		log.Fatalf("'users' is required")
	}

	log.Println("TURN Server is running at "+*publicIP+":"+strconv.Itoa(*port))
	
	// Create a UDP listener to pass into pion/turn
	// pion/turn itself doesn't allocate any UDP sockets, but lets the user pass them in
	// this allows us to add logging, storage or modify inbound/outbound traffic
	udpListener, err := net.ListenPacket("udp4", "0.0.0.0:"+strconv.Itoa(*port))

	if err != nil {
		log.Panicf("Failed to create TURN server listener: %s", err)
	}

	// Create a TCP listener to pass into pion/turn
	// pion/turn itself doesn't allocate any TCP listeners, but lets the user pass them in
	// this allows us to add logging, storage or modify inbound/outbound traffic
	tcpListener, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(*port))

	if err != nil {
		log.Panicf("Failed to create TURN server listener: %s", err)
	}

	// Cache -users flag for easy lookup later
	// If passwords are stored they should be saved to your DB hashed using turn.GenerateAuthKey
	usersMap := map[string][]byte{}
	for _, kv := range regexp.MustCompile(`(\w+)=(\w+)`).FindAllStringSubmatch(*users, -1) {
		usersMap[kv[1]] = turn.GenerateAuthKey(kv[1], *realm, kv[2])
	}

	s, err := turn.NewServer(turn.ServerConfig{
		Realm: *realm,
		// Set AuthHandler callback
		// This is called everytime a user tries to authenticate with the TURN server
		// Return the key for that user, or false when no user is found
		AuthHandler: func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			if key, ok := usersMap[username]; ok {
				return key, true
			}
			return nil, false
		},
		// PacketConnConfigs is a list of UDP Listeners and the configuration around them
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: net.ParseIP(*publicIP), // Claim that we are listening on IP passed by user (This should be your Public IP)
					Address:      "0.0.0.0",              // But actually be listening on every interface
				},
			},
		},
		// ListenerConfig is a list of Listeners and the configuration around them
		ListenerConfigs: []turn.ListenerConfig{
			{
				Listener: tcpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: net.ParseIP(*publicIP),
					Address:      "0.0.0.0",
				},
			},
		},
	})
	if err != nil {
		log.Panic(err)
	}

	// Block until user sends SIGINT or SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	if err = s.Close(); err != nil {
		log.Panic(err)
	}
}
