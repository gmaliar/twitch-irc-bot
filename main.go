package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"os"
	"os/signal"
	"syscall"

	"bytes"
	"encoding/binary"

	"github.com/boltdb/bolt"

	"github.com/nickvanw/ircx"
	"github.com/sorcix/irc"
)

var (
	name     = flag.String("name", os.Getenv("IRC_NICK"), "Nick to use in IRC")
	server   = flag.String("server", "irc.twitch.tv:6667", "Host:Port to connect to")
	user     = flag.String("user", os.Getenv("IRC_USER"), "Username")
	password = flag.String("password", os.Getenv("IRC_PASS"), "Password")
	channel  = flag.String("chan", os.Getenv("IRC_CHAN"), "Channels to join")
)

var db *bolt.DB

var users string = "users"
var messages string = "messages"

// Uint64ToByte converts an uint64 to bytes array in BigEndian
func Uint64ToByte(data uint64) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, data)
	return buf.Bytes()
}

// ByteToUint64 converts an uint64 in bytes to int64 with BigEndian
func ByteToUint64(data []byte) uint64 {
	var value uint64
	buf := bytes.NewReader(data)
	binary.Read(buf, binary.BigEndian, &value)
	return value
}

func init() {
	flag.Parse()
}

func main() {
	var err error
	db, err = bolt.Open("twitch.db", 0600, &bolt.Options{Timeout: 1 * time.Second})

	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	tx, err := db.Begin(true)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	b, err := tx.CreateBucketIfNotExists([]byte(*channel))
	if err != nil {
		log.Fatal(err)
	}

	_, err = b.CreateBucketIfNotExists([]byte(users))
	if err != nil {
		log.Fatal(err)
	}

	_, err = b.CreateBucketIfNotExists([]byte(messages))
	if err != nil {
		log.Fatal(err)
	}

	if err = tx.Commit(); err != nil {
		log.Fatal(err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		<-c
		db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(*channel))
			bu := b.Bucket([]byte(users))
			bm := b.Bucket([]byte(messages))

			bu.ForEach(func(k, v []byte) error {
				fmt.Printf("key=%s, value=%v\n", k, ByteToUint64(v))
				return nil
			})

			bm.ForEach(func(k, v []byte) error {
				fmt.Printf("key=%s, value=%v\n", k, ByteToUint64(v))
				return nil
			})

			return nil
		})
		os.Exit(1)
	}()

	bot := ircx.WithLogin(*server, *name, *user, *password)
	if err := bot.Connect(); err != nil {
		log.Panicln("Unable to dial IRC Server ", err)
	}

	RegisterHandlers(bot)
	bot.HandleLoop()
}

func RegisterHandlers(bot *ircx.Bot) {
	bot.HandleFunc(irc.RPL_WELCOME, RegisterConnect)
	bot.HandleFunc(irc.PING, PingHandler)
	bot.HandleFunc(irc.PRIVMSG, MsgHandler)
}

func RegisterConnect(s ircx.Sender, m *irc.Message) {
	log.Println(*user + " joining channel " + *channel)

	s.Send(&irc.Message{
		Command: irc.JOIN,
		Params:  []string{*channel},
	})
}

func PingHandler(s ircx.Sender, m *irc.Message) {
	log.Println("PONG")

	s.Send(&irc.Message{
		Command:  irc.PONG,
		Params:   m.Params,
		Trailing: m.Trailing,
	})
}

func MsgHandler(s ircx.Sender, m *irc.Message) {
	message := strings.Split(m.String(), " ")
	user := strings.Split(message[0], "!")[0][1:]

	err := db.Update(func(tx *bolt.Tx) error {
		words := make(map[string]uint64)
		message[3] = message[3][1:]

		for _, element := range message[3:] {
			words[element]++
		}

		b := tx.Bucket([]byte(*channel))
		bu := b.Bucket([]byte(users))
		bm := b.Bucket([]byte(messages))

		v := bu.Get([]byte(user))
		var count uint64 = 0
		if v != nil {
			count = ByteToUint64(v)
		}

		count = count + uint64(len(message[3:]))
		err := bu.Put([]byte(user), Uint64ToByte(count))
		if err != nil {
			return err
		}

		for word, count := range words {
			v := bm.Get([]byte(word))
			var sum uint64 = 0

			if v != nil {
				sum = ByteToUint64(v)
			}

			sum = sum + count
			err := bm.Put([]byte(word), Uint64ToByte(sum))
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		log.Fatal(err)
	} else {
		log.Println(user + " said " + strings.Join(message[3:], " "))
	}
}
