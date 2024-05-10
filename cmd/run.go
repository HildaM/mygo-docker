package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"

	"github.com/HildaM/mygo-docker/container"
	"github.com/HildaM/mygo-docker/utils"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	tty    bool
	detach bool
	cName  string // 容器名
	volume string // 挂载卷
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run container",
	Run: func(_ *cobra.Command, args []string) {
		cmd := args[0]
		logrus.Info("start run " + cmd)
		Run(cmd, tty)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().BoolVarP(&tty, "tty", "t", false, "enable tty")
	runCmd.Flags().BoolVarP(&detach, "detach", "d", false, "detach")
	runCmd.MarkFlagsMutuallyExclusive("tty", "detach")

}

func Run(cmd string, tty bool) {
	cID := utils.CreateCID()
	if cName == "" {
		cName = cID
	}

	parent, writePage, clean, err := NewParentProcess(tty)
	if err != nil {
		logrus.Error("new parent process error: " + err.Error())
		return
	}
	defer func() {
		if !tty || clean == nil {
			return
		}
		// 清理工作
		if err := clean(); err != nil {
			logrus.Error(errors.Wrap(err, "delete workspace").Error())
		}
	}()

	// 容器启动 Start() 会 clone 一个 namespace 隔离的进程
	// 然后在子进程中，调用 /proc/self/exe
	if err := parent.Start(); err != nil {
		logrus.Error(err.Error())
		return
	}

	// TODO 记录容器信息

	// TODO 创建cgroup manager并设置资源限制

	// writePage
	if _, err := writePage.WriteString(cmd); err != nil {
		logrus.Error("send cmd to child process error: " + err.Error())
		return
	}
	_ = writePage.Close()
	if tty {
		_ = parent.Wait()
	}
}

func NewParentProcess(tty bool) (*exec.Cmd, *os.File, utils.CleanFn, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}

	command := exec.Command("/proc/self/exe", "init")
	// execute command with namespace
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
	}

	// 把readPipe发送给子进程
	command.ExtraFiles = []*os.File{r}
	mntUrl := "/root/" + cName + "/mnt"
	cleanup, err := NewWorkSpace("/root/"+cName+"/", mntUrl)
	if err != nil {
		return nil, nil, cleanup, err
	}

	command.Dir = mntUrl
	if tty {
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
	} else {
		logFile, err := CreateLogFile(cName)
		if err != nil {
			return nil, nil, cleanup, errors.Wrap(err, "create log file")
		}
		command.Stdout = logFile
	}
	return command, w, cleanup, nil
}

var cleanFns []utils.CleanFn

var cleanUp = func() error {
	if len(cleanFns) == 0 {
		return nil
	}

	logrus.Info("do cleaning")
	i := len(cleanFns) - 1
	for i >= 0 {
		fn := cleanFns[i]
		if fn == nil {
			continue
		}

		if err := fn(); err != nil {
			return err
		}

		i--
	}

	return nil
}

// NewWorkSpace 文件系统准备
func NewWorkSpace(rootUrl, mntUrl string) (utils.CleanFn, error) {
	// readonly layer
	if err := CreateReadOnlyLayer(rootUrl); err != nil {
		return cleanUp, errors.Wrap(err, "create readonly layer")
	}

	// write layer
	if err := CollectCleanFn(CreateWriteLayer(rootUrl)); err != nil {
		return cleanUp, errors.Wrap(err, "create write layer")
	}

	// workdir
	if err := CollectCleanFn(utils.NewDir(rootUrl+"workdir/", 0777)); err != nil {
		return cleanUp, err
	}

	// create mnt dir
	if err := CollectCleanFn(utils.NewDir(mntUrl, 0777)); err != nil {
		return cleanUp, err
	}

	// mount mnt
	if err := CollectCleanFn(Mount(rootUrl, mntUrl)); err != nil {
		return cleanUp, err
	}

	// mount volume
	if volume != "" {
		src, dist, err := parseVolume(volume)
		if err != nil {
			return cleanUp, err
		}
		logrus.Debugf("mount volume %s to %s", src, dist)
		exists, err := utils.PathExists(src)
		if err != nil {
			return cleanUp, err
		}
		if !exists {
			logrus.Debugf("path %s don't exist, create", src)
			err := os.Mkdir(src, 0777)
			if err != nil {
				return cleanUp, err
			}
		}
		err = CollectCleanFn(utils.NewDir(mntUrl+dist, 0777))
		if err != nil {
			return cleanUp, err
		}
		// mount volume
		err = CollectCleanFn(MountDist(src, mntUrl+dist))
		if err != nil {
			return cleanUp, err
		}
	}
	return cleanUp, nil
}

func CreateReadOnlyLayer(rootUrl string) error {
	// 将busybox.tar解压到busybox目录下
	bbDir := rootUrl + "busybox/"
	exist, err := utils.PathExists(bbDir)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}
	err = os.MkdirAll(bbDir, 0777)
	if err != nil {
		return err
	}
	_, err = exec.Command("tar", "-xvf", "/root/busybox.tar", "-C", bbDir).CombinedOutput()
	return err
}

func CreateWriteLayer(rootUrl string) (utils.CleanFn, error) {
	// 创建writeLayer文件夹作为容器的唯一可写层
	return utils.NewDir(rootUrl+"writeLayer/", 0777)
}

// CollectCleanFn 收集cleanUp回收函数，待容器关闭后做资源回收
func CollectCleanFn(fn utils.CleanFn, err error) error {
	if err != nil {
		return err
	}
	// clean函数入列
	cleanFns = append(cleanFns, fn)
	return nil
}

func Mount(rootUrl string, mntUrl string) (utils.CleanFn, error) {
	// https://askubuntu.com/questions/109413/how-do-i-use-overlayfs
	option := fmt.Sprintf("upperdir=%swriteLayer,lowerdir=%sbusybox,workdir=%sworkdir", rootUrl, rootUrl, rootUrl)
	logrus.Infof("exec command: mount -t overlay -o %s none %s", option, mntUrl)
	cmd := exec.Command("mount", "-t", "overlay" /* ubuntu 不再支持aufs, 使用overlay*/, "-o", option, "none", mntUrl)
	unMount := func() error {
		logrus.Debug("unmount mnt")
		cmd := exec.Command("umount", mntUrl)
		utils.BindOutput(cmd)
		return cmd.Run()
	}
	utils.BindOutput(cmd)
	err := cmd.Run()
	return unMount, err
}

func parseVolume(volume string) (string, string, error) {
	arr := strings.Split(volume, ":")
	err := errors.New("volume option value: " + volume + " is not correct")
	if len(arr) < 2 {
		return "", "", err
	}
	src, dist := arr[0], arr[1]
	if src == "" || dist == "" {
		return "", "", err
	}
	return src, dist, nil
}

func MountDist(src, dist string) (utils.CleanFn, error) {
	cmd := exec.Command("mount", "--bind", src, dist)
	logrus.Debug(cmd.String())
	utils.BindOutput(cmd)
	return func() error {
		umount := exec.Command("umount", dist)
		logrus.Debug(umount.String())
		utils.BindOutput(umount)
		return umount.Run()
	}, cmd.Run()
}

// CreateLogFile 创建日志
func CreateLogFile(cName string) (*os.File, error) {
	// create dir
	dirPath := path.Join(container.DefaultInfoLocation, cName)
	if err := os.MkdirAll(dirPath, 0622); err != nil {
		return nil, err
	}

	// create file
	logFile := path.Join(dirPath, container.LogFile)
	return os.Create(logFile)
}
