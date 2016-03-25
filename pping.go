// Ping multiple hosts in parallel

// time notes: https://gobyexample.com/epoch

package main

import (
	"flag"
	"fmt"
	"github.com/jforman/carbon-golang"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	receiverHostFlag   string // receiverHost: Host running statistics daemon process
	receiverPortFlag   int    // receiverPort: Port running statistics daemon process
	receiverNoopFlag   bool   // If true, do not actually send the metrics to the receiver.
	receiverTypeFlag   string // receiverType: Set type of receiver for statistics
	hostsFlag          string // Flag hosts: Comma-seperated list of IP/hostnames to ping
	pingCountFlag      uint64 // Flag count: Uint8 Interger number of pings to send per cycle.
	oneshotFlag        bool
	intervalFlag       time.Duration
	verboseFlag        bool
	re_ping_packetloss *regexp.Regexp
	re_ping_rtt        *regexp.Regexp
	re_ping_hostname   *regexp.Regexp

	pingBinary string // Path to ping binary based upon operating system)
)

type PingStats struct {
	loss float64
	min  float64
	avg  float64
	max  float64
	mdev float64
}

type Ping struct {
	origin      string
	destination string
	time        int64
	stats       PingStats
}

func checkValidReceiverType(rType string, validTypes []string) bool {
	if rType == "" {
		return true
	}
	for _, v := range validTypes {
		if rType == v {
			return true
		}
	}
	return false
}

func setOsParams() {
	re_ping_hostname = regexp.MustCompile(`--- (?P<hostname>\S+) ping statistics ---`)

	switch runtime.GOOS {
	case "openbsd":
		pingBinary = "/sbin/ping"
		re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+.\d+)\% packet loss`)
		re_ping_rtt = regexp.MustCompile(`round-trip min/avg/max/std-dev = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
	case "linux":
		pingBinary = "/bin/ping"
		re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+)\% packet loss`)
		re_ping_rtt = regexp.MustCompile(`rtt min/avg/max/mdev = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
	}
}

func init() {
	flag.StringVar(&hostsFlag, "hosts", "", "Comma-seperated list of hosts to ping.")
	flag.Uint64Var(&pingCountFlag, "pingcount", 5, "Number of pings per cycle.")
	flag.BoolVar(&oneshotFlag, "oneshot", false, "Execute just one ping round per host. Do not loop.")
	flag.DurationVar(&intervalFlag, "interval", 60*time.Second, "Seconds of wait in between each round of pings.")
	flag.StringVar(&receiverHostFlag, "receiverhost", "", "Hostname of metrics receiver. Optional")
	flag.IntVar(&receiverPortFlag, "receiverport", 0, "Port of receiver.")
	flag.BoolVar(&receiverNoopFlag, "receivernoop", false, "If set, do not send Metrics to receiver.")
	flag.BoolVar(&verboseFlag, "v", false, "If set, print out metrics as they are processed.")
	flag.StringVar(&receiverTypeFlag, "receivertype", "", "Type of receiver for statistics. Optional.")

	setOsParams()
}

// Return true if host resolves, false if not.
func doesHostExist(host string) bool {
	addresses, _ := net.LookupHost(host)
	if len(addresses) > 0 {
		return true
	}
	return false
}

func getValidHosts(hosts []string) []string {
	var trimmedHosts []string
	for _, currentHost := range hosts {
		if doesHostExist(currentHost) {
			trimmedHosts = append(trimmedHosts, currentHost)
		}
	}
	return trimmedHosts
}

// https://github.com/StefanSchroeder/Golang-Regex-Tutorial/blob/master/01-chapter2.markdown
func processPingOutput(pingOutput string, pingErr bool) Ping {
	var ping Ping
	var stats PingStats
	now := time.Now()
	ping.time = now.Unix()
	origin, _ := os.Hostname()
	ping.origin = origin

	re_ping_hostname_matches := re_ping_hostname.FindAllStringSubmatch(pingOutput, -1)[0]
	ping.destination = re_ping_hostname_matches[1]

	re_packetloss_matches := re_ping_packetloss.FindAllStringSubmatch(pingOutput, -1)[0]
	stats.loss, _ = strconv.ParseFloat(re_packetloss_matches[1], 64)

	if pingErr == true {
		stats.min, stats.avg, stats.max, stats.mdev = 0, 0, 0, 0
	} else {
		re_rtt_matches := re_ping_rtt.FindAllStringSubmatch(pingOutput, -1)[0]
		stats.min, _ = strconv.ParseFloat(re_rtt_matches[1], 64)
		stats.avg, _ = strconv.ParseFloat(re_rtt_matches[2], 64)
		stats.max, _ = strconv.ParseFloat(re_rtt_matches[3], 64)
		stats.mdev, _ = strconv.ParseFloat(re_rtt_matches[4], 64)
	}
	ping.stats = stats
	if verboseFlag {
		log.Printf("Ping: %+v.\n", ping)
	}
	return ping
}

func executePing(host string, numPings uint64) (string, bool) {
	pingError := false
	countFlag := fmt.Sprintf("-c%v", numPings)
	out, err := exec.Command(pingBinary, countFlag, host).Output()
	if err != nil {
		log.Printf("Error with host %s, error: %s, output: %s.\n", host, err, out)
		pingError = true
	}
	s_out := string(out[:])
	return s_out, pingError
}

func spawnPingLoop(c chan<- Ping,
	host string,
	numPings uint64,
	sleepTime time.Duration,
	oneshot bool) {
	for {
		raw_output, err := executePing(host, numPings)
		pingResult := processPingOutput(raw_output, err)
		c <- pingResult
		time.Sleep(sleepTime)

		if oneshot == true {
			break
		}
	}
}

func createCarbonMetrics(ping Ping) []carbon.Metric {
	var out []carbon.Metric
	formattedDestination := strings.Replace(ping.destination, ".", "_", -1)
	prefix := fmt.Sprintf("ping.%v.%v", ping.origin, formattedDestination)
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.loss", prefix), Value: ping.stats.loss, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.min", prefix), Value: ping.stats.min, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.avg", prefix), Value: ping.stats.avg, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.max", prefix), Value: ping.stats.max, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.mdev", prefix), Value: ping.stats.mdev, Timestamp: ping.time})
	return out
}

func processPing(c <-chan Ping) error {
	var err error
	var carbonReceiver *carbon.Carbon

	switch receiverTypeFlag {
	case "carbon":
		carbonReceiver, err = carbon.NewCarbon(receiverHostFlag, receiverPortFlag, receiverNoopFlag, verboseFlag)
	case "influxdb":
		log.Printf("TODO: Create new influxdb receiver.\n")
	}

	if err != nil {
		return err
	}
	for {
		pingResult := <-c
		switch receiverTypeFlag {
		case "carbon":
			carbonReceiver.SendMetrics(createCarbonMetrics(pingResult))
		case "influxdb":
			log.Println("TODO: Send to influxdb!")
		}
	}
	return nil
}

func main() {
	flag.Parse()
	hasValidReceiver := checkValidReceiverType(receiverTypeFlag, []string{"carbon", "influxdb"})
	if !hasValidReceiver {
		log.Fatalf("You specified an unsupported receiver type %v.\n", receiverTypeFlag)
	}

	hosts := strings.Split(hostsFlag, ",")
	validHosts := getValidHosts(hosts)
	log.Printf("Hosts to ping: %v\n", validHosts)
	var c chan Ping = make(chan Ping)
	for _, currentHost := range validHosts {
		log.Printf("Spawning ping loop for host %v.\n", currentHost)
		go spawnPingLoop(c, currentHost, pingCountFlag, intervalFlag, oneshotFlag)
	}
	processPing(c)
}
