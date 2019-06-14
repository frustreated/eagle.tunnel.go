/*
 * @Author: EagleXiang
 * @Github: https://github.com/eaglexiang
 * @Date: 2018-12-27 08:38:06
 * @LastEditors: EagleXiang
 * @LastEditTime: 2019-06-14 20:59:23
 */

package main

import (
	"fmt"
	"os"
	"os/signal"

	mycmd "github.com/eaglexiang/eagle.tunnel.go/src/cmd"
	etcore "github.com/eaglexiang/eagle.tunnel.go/src/core/core"
	"github.com/eaglexiang/eagle.tunnel.go/src/logger"
	settings "github.com/eaglexiang/go-settings"
)

var service *etcore.Service

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("error: no arg for Eagle Tunnel")
		return
	}

	switch args[0] {
	case "check":
		check(args)
	default:
		core(args)
	}
}

func waitSig() {
	fmt.Println("press Ctrl + C to quit")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

// Init 初始化参数系统
func Init(args []string) error {
	err := mycmd.ImportArgs(args)
	if err != nil {
		return err
	}
	return etcore.ImportConfig()
}

// check check子命令
func check(args []string) {
	err := Init(args[2:])
	if err != nil {
		logger.Error(err)
		return
	}
	mycmd.Check(args[1])
}

func core(args []string) {
	err := Init(args)
	if err != nil {
		if err.Error() != "no need to continue" {
			logger.Error(err)
		}
		return
	}
	fmt.Println(settings.ToString())
	service = etcore.CreateService()
	defer service.Close()
	go startService()
	waitSig()
	fmt.Println("stoping...")
	stopService()
}

func startService() {
	err := service.Start()
	if err != nil {
		logger.Error(err)
	}
}

func stopService() {
	service.Close()
}
