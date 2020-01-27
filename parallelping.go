// Ping multiple hosts in parallel

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	destinationFlag    string // Flag hosts: Comma-seperated list of IP/hostnames to ping
	pingCountFlag      uint64 // Flag count: Uint8 Interger number of pings to send per cycle.
	oneshotFlag        bool
	originFlag         string // String denoting specific origin hostname used in metric submission.
	intervalFlag       time.Duration
	verboseFlag        bool
	re_ping_packetloss *regexp.Regexp
	re_ping_rtt        *regexp.Regexp
	re_ping_hostname   *regexp.Regexp
	quietFlag          bool

	pingBinary string // Path to ping binary based upon operating system)

	ping_rtt_min_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_min_ms",
			Help: "Ping rtt minimum in ms.",
		},
		[]string{
			"destination",
			"hostname",
		},
	)
	ping_rtt_avg_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_avg_ms",
			Help: "Ping rtt average in ms.",
		},
		[]string{
			"destination",
			"hostname",
		},
	)
	ping_rtt_max_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_max_ms",
			Help: "Ping rtt maximum in ms.",
		},
		[]string{
			"destination",
			"hostname",
		},
	)
	ping_rtt_mdev_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_mdev_ms",
			Help: "Ping rtt standard deviation in ms.",
		},
		[]string{
			"destination",
			"hostname",
		},
	)
	ping_loss_pct = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_loss_pct",
			Help: "Ping rtt loss in percent.",
		},
		[]string{
			"destination",
			"hostname",
		},
	)
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
	hostname    string // Derived DNS hostname for destination.
	time        int64
	stats       PingStats
}

func getDistro() string {
	contents, _ := ioutil.ReadFile("/etc/os-release")
	re_distro := regexp.MustCompile("ID=([a-zA-Z]+)")

	str_contents := string(contents)
	if verboseFlag {
		log.Printf("Contents of /etc/os-release: %v.\n", str_contents)
	}
	distro := re_distro.FindStringSubmatch(str_contents)[1]

	if verboseFlag {
		log.Printf("Distribution determined to be: %v.\n", distro)
	}
	return distro
}

func setOsParams() {
	re_ping_hostname = regexp.MustCompile(`--- (?P<hostname>\S+) ping statistics ---`)
	if !quietFlag {
		log.Printf("Operating System determined to be: %v.\n", runtime.GOOS)
	}
	switch runtime.GOOS {
	case "openbsd":
		pingBinary = "/sbin777/ping"
		re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+.\d+)\% packet loss`)
		re_ping_rtt = regexp.MustCompile(`round-trip min/avg/max/std-dev = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
	case "linux":
		pingBinary = "/bin/ping"
		re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+)\% packet loss`)
		distro := getDistro()
		switch distro {
		case "ubuntu", "debian":
			re_ping_rtt = regexp.MustCompile(`(rtt|round-trip) min/avg/max/(mdev|stddev) = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
		case "alpine":
			re_ping_rtt = regexp.MustCompile(`round-trip min/avg/max = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+) ms`)
		}
	default:
		log.Fatalf("Unsupported operating system. runtime.GOOS: %v.\n", runtime.GOOS)
	}
}

func init() {
	flag.StringVar(&destinationFlag, "destination", "", "Comma-seperated list of destinations to ping.")
	flag.Uint64Var(&pingCountFlag, "pingcount", 5, "Number of pings per cycle.")
	flag.BoolVar(&oneshotFlag, "oneshot", false, "Execute just one ping round per host. Do not loop.")
	flag.StringVar(&originFlag, "origin", "", "Override hostname as origin with this value.")
	flag.DurationVar(&intervalFlag, "interval", 60*time.Second, "Seconds of wait in between each round of pings.")
	flag.BoolVar(&verboseFlag, "v", false, "If set, print out metrics as they are processed.")
	flag.BoolVar(&quietFlag, "q", false, "If set, only log in case of errors.")
}

// Return true if desination resolves, false if not.
func doesDestinationExist(d string) bool {
	addresses, _ := net.LookupHost(d)
	if len(addresses) > 0 {
		return true
	}
	return false
}

func getValidDestinations(d []string) []string {
	var vd []string
	for _, cd := range d {
		if doesDestinationExist(cd) {
			vd = append(vd, cd)
		}
	}
	return vd
}

func processPingOutput(pingOutput string, pingErr bool) Ping {
	var ping Ping
	var stats PingStats
	now := time.Now()
	ping.time = now.Unix()
	if len(originFlag) == 0 {
		origin, _ := os.Hostname()
		ping.origin = origin
	} else {
		ping.origin = originFlag
	}

	re_ping_hostname_matches := re_ping_hostname.FindAllStringSubmatch(pingOutput, -1)[0]
	ping.hostname = re_ping_hostname_matches[1]

	re_packetloss_matches := re_ping_packetloss.FindAllStringSubmatch(pingOutput, -1)[0]

	stats.loss, _ = strconv.ParseFloat(re_packetloss_matches[1], 64)

	if pingErr == true {
		stats.min, stats.avg, stats.max, stats.mdev = 0, 0, 0, 0
	} else {
		re_rtt_matches := re_ping_rtt.FindAllStringSubmatch(pingOutput, -1)[0]
		rtt_map := make(map[string]string)
		for i, name := range re_ping_rtt.SubexpNames() {
			if i != 0 {
				rtt_map[name] = re_rtt_matches[i]
			}
		}
		stats.min, _ = strconv.ParseFloat(rtt_map["min"], 64)
		stats.avg, _ = strconv.ParseFloat(rtt_map["avg"], 64)
		stats.max, _ = strconv.ParseFloat(rtt_map["max"], 64)
		stats.mdev, _ = strconv.ParseFloat(rtt_map["mdev"], 64)
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
	if verboseFlag {
		log.Printf("Raw Ping Output: %v.\n", s_out)
	}
	return s_out, pingError
}

func spawnPingLoop(c chan<- Ping,
	wg *sync.WaitGroup,
	destination string,
	numPings uint64,
	sleepTime time.Duration,
	oneshot bool) {
	log.Printf("Spawning ping loop for %s.\n", destination)
	for {
		raw_output, err := executePing(destination, numPings)
		pingResult := processPingOutput(raw_output, err)
		pingResult.destination = destination
		c <- pingResult
		if oneshot {
			wg.Done()
		} else {
			time.Sleep(sleepTime)
		}
	}
}

func updatePrometheusMetrics(ping Ping) {
	ping_rtt_min_ms.With(prometheus.Labels{
		"destination": ping.destination,
		"hostname":    ping.hostname,
	}).Set(ping.stats.min)
	ping_rtt_avg_ms.With(prometheus.Labels{
		"destination": ping.destination,
		"hostname":    ping.hostname,
	}).Set(ping.stats.avg)
	ping_rtt_max_ms.With(prometheus.Labels{
		"destination": ping.destination,
		"hostname":    ping.hostname,
	}).Set(ping.stats.max)
	ping_rtt_mdev_ms.With(prometheus.Labels{
		"destination": ping.destination,
		"hostname":    ping.hostname,
	}).Set(ping.stats.mdev)
	ping_loss_pct.With(prometheus.Labels{
		"destination": ping.destination,
		"hostname":    ping.hostname,
	}).Set(ping.stats.loss)
}

func processPing(c <-chan Ping) {
	for {
		pr := <-c
		if verboseFlag {
			log.Printf("pingResult: %+v.\n", pr)
		}
		updatePrometheusMetrics(pr)
	}
}

func main() {
	flag.Parse()
	setOsParams()
	var wg sync.WaitGroup
	defer wg.Wait()

	destinations := strings.Split(destinationFlag, ",")
	validDestinations := getValidDestinations(destinations)
	log.Printf("Destinations to ping: %v.\n", validDestinations)
	var c chan Ping = make(chan Ping)
	for _, cd := range validDestinations {
		wg.Add(1)
		go func(dest string) {
			spawnPingLoop(c, &wg, dest, pingCountFlag, intervalFlag, oneshotFlag)
		}(cd)
	}

	go processPing(c)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":2112", nil); err != nil {
			log.Fatal("HttpServer: ListenAndServe() error: " + err.Error())
		}
	}()
}
