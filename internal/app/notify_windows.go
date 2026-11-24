//go:build windows

package app

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// toastNotify Windows 10/11 系统通知（PowerShell WinRT Toast，尽力而为）。
// 失败仅写日志，绝不阻塞主流程。
func toastNotify(title, body string) {
	const scriptTemplate = `$ErrorActionPreference='SilentlyContinue'
[Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime] | Out-Null
$x=New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml('<toast><visual><binding template="ToastGeneric"><text>%s</text><text>%s</text></binding></visual></toast>')
$t=New-Object Windows.UI.Notifications.ToastNotification $x
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('Microsoft.Windows.PowerShell').Show($t)
`
	script := fmt.Sprintf(scriptTemplate, xmlEscape(title), xmlEscape(body))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err := cmd.Run(); err != nil {
		log.Printf("toast 通知失败（忽略）: %v", err)
	}
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&apos;", `"`, "&quot;")
	return r.Replace(s)
}
