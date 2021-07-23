package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/livepeer/healthy-streams/api"
	"github.com/livepeer/healthy-streams/event"
	"github.com/livepeer/healthy-streams/health"
	"github.com/peterbourgon/ff"
)

var (
	host string
	port uint

	rabbitmqStreamUri, amqpUri string
	streamingOpts              = health.StreamingOptions{}
)

func init() {
	flag.Set("logtostderr", "true")
	fs := flag.NewFlagSet("healthstream", flag.ExitOnError)

	glogVFlag := flag.Lookup("v")
	verbosity := fs.Int("v", 0, "Log verbosity {0-10}")

	fs.StringVar(&host, "host", "localhost", "Hostname to bind to")
	fs.UintVar(&port, "port", 8080, "Port to listen on")

	// Streaming options
	fs.StringVar(&rabbitmqStreamUri, "rabbitmqStreamUri", "rabbitmq-stream://guest:guest@localhost:5552/livepeer", "Rabbitmq-stream URI to consume from")
	fs.StringVar(&amqpUri, "amqpUri", "", "Explicit AMQP URI in case of non-default protocols/ports (optional). Must point to the same cluster as rabbitmqStreamUri")
	fs.StringVar(&streamingOpts.Stream, "streamName", "lp_stream_health_v0", "Name of RabbitMQ stream to create and consume from")
	fs.StringVar(&streamingOpts.Exchange, "exchange", "lp_golivepeer_metadata", "Name of RabbitMQ exchange to bind the stream to on creation")
	fs.StringVar(&streamingOpts.ConsumerName, "consumerName", "", `Consumer name to use when consuming stream (default "healthy-streams-${hostname}")`)
	fs.StringVar(&streamingOpts.MaxLengthBytes, "streamMaxLength", "50gb", "When creating a new stream, config for max total storage size")
	fs.StringVar(&streamingOpts.MaxSegmentSizeBytes, "streamMaxSegmentSize", "500mb", "When creating a new stream, config for max stream segment size in storage")
	fs.StringVar(&streamingOpts.MaxAge, "streamMaxAge", "720h", `When creating a new stream, config for max age of stored events`)

	fs.String("config", "", "config file (optional)")
	ff.Parse(fs, os.Args[1:],
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.PlainParser),
		ff.WithEnvVarPrefix("LP"),
	)
	flag.CommandLine.Parse(nil)
	glogVFlag.Value.Set(strconv.Itoa(*verbosity))

	if streamingOpts.ConsumerName == "" {
		streamingOpts.ConsumerName = "healthy-streams-" + hostname()
	}
}

func main() {
	glog.Info("Stream health care system starting up...")
	ctx := contextUntilSignal(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	consumer, err := event.NewStreamConsumer(rabbitmqStreamUri, amqpUri)
	if err != nil {
		glog.Fatalf("Error creating stream consumer. err=%q", err)
	}
	defer consumer.Stop()

	healthcore := health.NewCore(health.CoreOptions{Streaming: streamingOpts}, consumer)
	if err := healthcore.Start(ctx); err != nil {
		glog.Fatalf("Error starting health core. err=%q", err)
	}

	glog.Info("Starting server...")
	err = api.ListenAndServe(ctx, host, port, 1*time.Second, healthcore)
	if err != nil {
		glog.Fatalf("Error starting api server. err=%q", err)
	}
}

func contextUntilSignal(parent context.Context, sigs ...os.Signal) context.Context {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		defer cancel()
		waitSignal(sigs...)
	}()
	return ctx
}

func waitSignal(sigs ...os.Signal) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, sigs...)
	defer signal.Stop(sigc)

	signal := <-sigc
	switch signal {
	case syscall.SIGINT:
		glog.Infof("Got Ctrl-C, shutting down")
	case syscall.SIGTERM:
		glog.Infof("Got SIGTERM, shutting down")
	default:
		glog.Infof("Got signal %d, shutting down", signal)
	}
}

func hostname() string {
	host, err := os.Hostname()
	if err != nil {
		glog.Fatalf("Failed to read hostname. err=%q", err)
	}
	return host
}
