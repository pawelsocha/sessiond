package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/pawelsocha/kryptond/client"
	"github.com/pawelsocha/kryptond/config"
	"github.com/pawelsocha/kryptond/database"
	. "github.com/pawelsocha/kryptond/logging"
	"github.com/pawelsocha/kryptond/mikrotik"
	"github.com/pawelsocha/kryptond/router"
	//mysql
	_ "github.com/jinzhu/gorm/dialects/mysql"
)

type QueueStats struct {
	ID      string `routeros:".id"`
	Packets string `routeros:"packets"`
	Bytes   string `routeros:"bytes"`
	Dropped string `routeros:"dropped"`
	Limits  string `routeros:"max-limit"`
	Comment string `routeros:"comment"`
	IP      string `routeros:"target"`
}

func (q QueueStats) GetId() string {
	return q.ID
}

func (q QueueStats) Where() string {
	return ""
}

func (q QueueStats) Path() string {
	return "/queue/simple"
}

func (q QueueStats) PrintAttrs() string {
	return "stats"
}

func (q QueueStats) GetNode() int64 {
	width := 0
	for i, r := range q.Comment {

		if r == ':' {
			width = i + 1
			continue
		}
		if r == ' ' {
			u, err := strconv.ParseInt(q.Comment[width:i], 10, 64)
			if err != nil {
				Log.Errorf("Can't convert %s to int.", q.Comment[width:i])
				return 0
			}
			return u
			break
		}
	}
	return 0
}

func (q QueueStats) GetClient() int64 {
	for i, r := range q.Comment {
		if r == ':' {
			u, err := strconv.ParseInt(q.Comment[0:i], 10, 64)
			if err != nil {
				Log.Errorf("Can't convert %s to int.", q.Comment[0:i])
				return 0
			}
			return u
		}
	}
	return 0
}

func (q QueueStats) GetAddress() uint32 {
	ipv4, _, err := net.ParseCIDR(q.IP)

	if err != nil {
		Log.Errorf("Can't convert %s to network. Error: %s", q.IP, err)
		return 0
	}
	parsed := ipv4.To4()
	return binary.BigEndian.Uint32(parsed)
}

func (q QueueStats) GetUpload() uint64 {
	upload := strings.Split(q.Bytes, "/")[0]

	u, err := strconv.ParseUint(upload, 10, 64)
	if err != nil {
		Log.Errorf("Can't convert upload %s to int.", upload)
		return 0
	}
	return u
}

func (q QueueStats) GetDownload() uint64 {
	download := strings.Split(q.Bytes, "/")[1]

	u, err := strconv.ParseUint(download, 10, 64)
	if err != nil {
		Log.Errorf("Can't convert download %s to int.", download)
		return 0
	}
	return u
}

func main() {
	config, err := config.New(ConfigFile)

	if err != nil {
		Log.Critical("Can't read configuration. Error: ", err)
		return
	}

	database.Connection, err = gorm.Open("mysql", config.GetDatabaseDSN())

	if err != nil {
		Log.Critical("Can't connect to database. Error:", err)
		return
	}

	routers, err := router.GetRoutersList()
	if err != nil {
		Log.Critical("Can't get list of routers from database. Error:", err)
		return
	}

	queryEntity := QueueStats{}

	for _, router := range routers {
		device, err := mikrotik.NewDevice(router.PublicAddress)
		if err != nil {
			Log.Criticalf("Can't prepare new routeros device. Error: %s", err)
		}

		ret, err := device.ExecuteEntity("print", queryEntity)
		if err != nil {
			Log.Criticalf("Can't get queue stats from routeros. Error: %s", err)
		}

		for _, record := range ret.Re {
			stats := QueueStats{}
			record.Unmarshal(&stats)
			session := client.Session{
				CustomerId: stats.GetClient(),
				NodeId:     stats.GetNode(),
				IP:         stats.GetAddress(),
				Download:   stats.GetDownload(),
				Upload:     stats.GetUpload(),
				Start:      (uint64(time.Now().Unix()) - (5 * 60)),
				Stop:       uint64(time.Now().Unix()),
			}

			if session.Upload == 0 && session.Download == 0 {
				continue
			}
			Log.Infof("session: %#v", session)
			if err := session.Save(); err != nil {
				Log.Criticalf("Can't store node session in db. Error: %s", err)
				continue
			}

			reset, err := device.Execute("/queue/simple/reset-counters", fmt.Sprintf("=.id=%s", stats.ID))
			Log.Infof("reset: %#v, err: %s", reset, err)
		}
	}
}
