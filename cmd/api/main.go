package main

import (
	"context"
	"database/sql"
	"expvar"
	"flag"
	_ "github.com/lib/pq"
	"greenlight.mateus.cardoso.com/internal/data"
	"greenlight.mateus.cardoso.com/internal/mailer"
	"log"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const version = "1.0.0"

type config struct {
	port int
	env  string
	db   struct {
		dsn          string
		maxOpenConns int
		maxIdleConns int
		maxIdleTime  time.Duration
	}
	limiter struct {
		rps    float64
		burst  int
		enable bool
	}
	smtp struct {
		host     string
		port     int
		username string
		password string
		sender   string
	}

	cors struct {
		trustedOrigins []string
	}
}

type application struct {
	config config
	logger *slog.Logger
	models data.Models
	mailer mailer.Mailer
	wg     sync.WaitGroup
}

func main() {
	var config config

	flag.IntVar(&config.port, "port", 4000, "API server port")
	flag.StringVar(&config.env, "env", "development", "Environment (development|staging|production)")
	flag.StringVar(&config.db.dsn, "db-dsn", "", "PostgreSQL DSN")
	flag.IntVar(&config.db.maxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	flag.IntVar(&config.db.maxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	flag.DurationVar(&config.db.maxIdleTime, "db-max-idle-time", 15*time.Minute, "PostgreSQL max connection idle time")
	flag.Float64Var(&config.limiter.rps, "limiter-rps", 2, "Rate limiter maximum requests per second")
	flag.IntVar(&config.limiter.burst, "limiter-burst", 4, "Rate limiter maximum burst")
	flag.BoolVar(&config.limiter.enable, "limiter-enabled", true, "Enable rate limiter")
	flag.StringVar(&config.smtp.host, "smtp-host", "sandbox.smtp.mailtrap.io", "SMTP Host")
	flag.IntVar(&config.smtp.port, "smtp-port", 2525, "SMTP port")
	flag.StringVar(&config.smtp.username, "smtp-username", "45df17c270f1b3", "SMTP username")
	flag.StringVar(&config.smtp.password, "smtp-password", "2f0711ef4e9f21", "SMTP password")
	flag.StringVar(&config.smtp.sender, "smtp-sender", "Greenlight <no-reply@greenlight.mateuscardoso.net>", "SMTP sender")
	flag.Func("cors-trusted-origins", "Trusted CORS origins (space separeted)", func(val string) error {
		config.cors.trustedOrigins = strings.Fields(val)
		return nil
	})
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := openDB(config)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	defer db.Close()

	logger.Info("database connection pool established")

	expvar.NewString("version").Set(version)

	expvar.Publish("goroutines", expvar.Func(func() any {
		return runtime.NumGoroutine()
	}))

	expvar.Publish("database", expvar.Func(func() any {
		return db.Stats()
	}))

	expvar.Publish("timestamp", expvar.Func(func() any {
		return time.Now().Unix()
	}))

	app := &application{
		config: config,
		logger: logger,
		models: data.NewModels(db),
		mailer: mailer.New(config.smtp.host, config.smtp.port, config.smtp.username, config.smtp.password, config.smtp.sender),
	}

	err = app.serve()
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

func getLocalIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddress := conn.LocalAddr().(*net.UDPAddr)

	return localAddress.IP
}

func openDB(config config) (*sql.DB, error) {
	db, err := sql.Open("postgres", config.db.dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(config.db.maxOpenConns)
	db.SetMaxIdleConns(config.db.maxIdleConns)
	db.SetConnMaxIdleTime(config.db.maxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()

	err = db.PingContext(ctx)
	if err != nil {
		return nil, err
	}

	return db, nil
}
