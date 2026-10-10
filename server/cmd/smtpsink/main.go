// Command smtpsink — приёмник писем для нагрузочных замеров (срез 27): принимает всё, ничего не хранит, считает.
// Задержка на каждый ответ имитирует расстояние до настоящего почтового сервиса: у Mailpit на этом же компьютере
// диалог занимает доли миллисекунды, а у внешнего сервиса десятки, и скорость очереди писем зависит от неё.
//
//	go run ./cmd/smtpsink -addr 127.0.0.1:1026 -delay 25ms
//
// Раз в 5 секунд и при остановке печатает, сколько писем принято и сколько разных получателей (дубль виден по разнице).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:1026", "адрес приёмника")
	delay := flag.Duration("delay", 0, "пауза перед каждым ответом (расстояние до почтового сервиса)")
	flag.Parse()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	var (
		accepted  atomic.Int64
		conns     atomic.Int64
		mu        sync.Mutex
		recipient = map[string]int{}
	)
	report := func() {
		mu.Lock()
		defer mu.Unlock()
		dup := 0
		for _, n := range recipient {
			if n > 1 {
				dup += n - 1
			}
		}
		fmt.Printf("%s принято %d, получателей %d, повторов %d, соединений %d\n",
			time.Now().Format("15:04:05"), accepted.Load(), len(recipient), dup, conns.Load())
	}
	go func() {
		for range time.Tick(5 * time.Second) {
			report()
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		report()
		os.Exit(0)
	}()

	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		conns.Add(1)
		go func() {
			defer func() { _ = c.Close() }()
			serve(c, *delay, func(rcpt string) {
				accepted.Add(1)
				mu.Lock()
				recipient[rcpt]++
				mu.Unlock()
			})
		}()
	}
}

func serve(c net.Conn, delay time.Duration, got func(rcpt string)) {
	r := bufio.NewReader(c)
	reply := func(s string) bool {
		if delay > 0 {
			time.Sleep(delay)
		}
		_, err := fmt.Fprintf(c, "%s\r\n", s)
		return err == nil
	}
	if !reply("220 smtpsink ready") {
		return
	}
	var rcpt string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		var ok bool
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			ok = reply("250 smtpsink")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			ok = reply("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO"):
			rcpt = strings.ToLower(strings.Trim(strings.TrimSpace(line[len("RCPT TO:"):]), "<>"))
			ok = reply("250 ok")
		case cmd == "DATA":
			if !reply("354 go ahead") {
				return
			}
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
			}
			got(rcpt)
			ok = reply("250 queued")
		case cmd == "RSET", cmd == "NOOP":
			ok = reply("250 ok")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			ok = reply("502 not implemented")
		}
		if !ok {
			return
		}
	}
}
