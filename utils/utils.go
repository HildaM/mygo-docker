package utils

import (
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// CreateCID 生成的容器ID是一个以"c"开头，后跟时间戳的36进制表示的字符串
func CreateCID() string {
	return "c" + strconv.FormatInt(time.Now().UnixMilli(), 36)
}

// 清理函数
type CleanFn = func() error

var CleanFnNil = func() error {
	return nil
}

// 与文件处理相关的函数
func NewDir(dirPath string, perm os.FileMode) (cleanFn CleanFn, err error) {
	err = os.MkdirAll(dirPath, perm)
	cleanFn = func() error {
		logrus.Debug("remove dir: " + dirPath)
		return os.RemoveAll(dirPath)
	}
	return
}

func PathExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

// os系统相关的工具
func BindOutput(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
}
