//go:build !windows && !darwin

package services

import "errors"

func (s *UpdateService) launchInstaller(string) error {
	return errors.New("当前平台暂不支持自动安装，请前往发布页手动下载")
}

func windowsInstalledByMSI() bool { return false }
