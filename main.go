package main

import (
	"github.com/HildaM/mygo-docker/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.DebugLevel)
	cmd.Execute()
}
