Parallel Ping
=============

# What is it?

A Go-binary used to ping remote hosts and return the min/max/avg/dev data as a Struct for later processing. Pings are run simultaneously in parallel, as serializing pings to multiple hosts does not bode well for getting consistent metrics. This Go binary integrates tightly with http://www.github.com/jforman/carbon-golang to send these Ping metrics back to a Carbon Cache for visualization within Graphite.

# How do I use it?

After putting the repository for both this binary and the carbon-golang library in your GOWORK path, it is as simple as executing pping.go.

### Help Output

```bash
$ go run pping.go --help
  -carbonhost string
    	Hostname of carbon receiver. Optional
  -carbonnoop
    	If set, do not send Metrics to Carbon.
  -carbonport int
    	Port of carbon receiver.
  -hosts string
    	Comma-seperated list of hosts to ping.
  -interval duration
    	Seconds of wait in between each round of pings. (default 1m0s)
  -oneshot
    	Execute just one ping round per host. Do not loop.
  -pingcount uint
    	Number of pings per cycle. (default 5)
```

The help output should be self explanatory, both with required and optional parameters. 

An example of a parallel ping execution:

```bash
$ go run pping.go -hosts www.facebook.com,www.google.com -pingcount 3 -interval 30s  -v

Hosts to ping: [www.facebook.com www.google.com]

Spawning ping loop for host www.facebook.com.
Spawning ping loop for host www.google.com.

Ping: {oyster www.google.com 1456662993 {0 44.952 45.354 46.049 0.493}}.
Carbon Metrics: [ping.oyster.www_google_com.loss 0.000000 1456662993 ping.oyster.www_google_com.min 44.952000 1456662993 ping.oyster.www_google_com.avg 45.354000 1456662993 ping.oyster.www_google_com.max 46.049000 1456662993 ping.oyster.www_google_com.mdev 0.493000 1456662993].

Ping: {oyster star-mini.c10r.facebook.com 1456662993 {0 83.383 83.618 84.011 0.435}}.
Carbon Metrics: [ping.oyster.star-mini_c10r_facebook_com.loss 0.000000 1456662993 ping.oyster.star-mini_c10r_facebook_com.min 83.383000 1456662993 ping.oyster.star-mini_c10r_facebook_com.avg 83.618000 1456662993 ping.oyster.star-mini_c10r_facebook_com.max 84.011000 1456662993 ping.oyster.star-mini_c10r_facebook_com.mdev 0.435000 1456662993].
```

The `-v` flag adds aggregate-Ping output like below if you wish:

```bash
Ping: {oyster www.google.com 1456662993 {0 44.952 45.354 46.049 0.493}}.
```

## Integrating with carbon-golang

If you wish to send your Ping metrics to Carbon for eventual visualization in Graphite or other frontends, the required flags are `carbonhost` and `carbonport`. It is important to know that carbonport is the line receiever port and not the pickle receiver port.

Metric structure is as follows:

```bash
ping.{originating_host}.{destination_host}.{ping_metric}
```

Where ping metric is one of min, max, avg, and stddev.
