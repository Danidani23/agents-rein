//go:build app

package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/localstate"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/owner"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/web"
	"github.com/Danidani23/agents-rein/internal/version"
)

// appBuild says this rein carries the app (D50): built with the app tag
// and cgo, it is the app when started without a command.
const appBuild = true

// appIdentifier names the app to the operating system: its single
// instance, its bundle and its notifications.
const appIdentifier = "io.github.danidani23.rein"

// openCategory is the notifications whose button opens the page they name
// under the key page.
const openCategory = "open-page"

// runApp is the app: the background process of rein's home, which never
// ends while the app runs and stays for a client of another version, plus
// a window showing the pages of rein ui, an icon in the menu bar or tray,
// notifications, one instance at a time, and a start at login the
// developer turns on.
func runApp(arguments []string, errOut io.Writer) int {
	background := len(arguments) > 0 && arguments[0] == backgroundFlag
	home, err := localstate.Home()
	if err != nil {
		fmt.Fprintf(errOut, "rein app: %v\n", err)
		return cli.ExitCrashed
	}
	err = os.MkdirAll(home, 0o755)
	if err != nil {
		fmt.Fprintf(errOut, "rein app: %v\n", err)
		return cli.ExitCrashed
	}
	logFile, err := os.OpenFile(ownerLog(home), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintf(errOut, "rein app: %v\n", err)
		return cli.ExitCrashed
	}
	defer logFile.Close()
	executable, _ := os.Executable()
	orchestrator, err := wireFor(execute.Runner{}, executable)()
	if err != nil {
		fmt.Fprintf(logFile, "rein app: %v\n", err)
		return cli.ExitCrashed
	}
	pages, err := web.Window(orchestrator, version.String(), web.Interrupt{Stop: execute.Interrupt, Running: execute.Running})
	if err != nil {
		fmt.Fprintf(logFile, "rein app: %v\n", err)
		return cli.ExitCrashed
	}
	identity := buildIdentity()
	server := &owner.Server{
		Identity: identity,
		Accepts:  func(client string) bool { return owner.VersionOf(client) == owner.VersionOf(identity) },
		Stays:    true,
		Run:      runSession,
		Log:      logFile,
	}
	owned := make(chan struct{})
	notifier := notifications.New()
	var window *application.WebviewWindow
	show := func(page string) {
		if page == "" {
			page = "/"
		}
		window.SetURL(page)
		window.Show()
		window.Focus()
	}
	app := application.New(application.Options{
		Name:        "rein",
		Description: "Agents Rein: the settings of your AI coding agents, from your organisation's repository",
		Mac:         application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		Assets:      application.AssetOptions{Handler: pages, DisableLogging: true},
		Services:    []application.Service{application.NewService(notifier)},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               appIdentifier,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { show("/") },
		},
		Logger:   slog.New(slog.NewTextHandler(logFile, nil)),
		LogLevel: slog.LevelWarn,
		// the commands running finish before the app ends; no new one starts
		OnShutdown: func() {
			server.Stop()
			<-owned
		},
	})
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "rein",
		URL:    "/",
		Width:  1100,
		Height: 760,
		Hidden: background,
	})
	// closing the window leaves the app in the menu bar, the owner running
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		window.Hide()
		event.Cancel()
	})
	tray := app.SystemTray.New()
	tray.SetTooltip("rein")
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIcon())
	} else {
		tray.SetIcon(trayIcon())
	}
	menu := app.NewMenu()
	menu.Add("Open rein").OnClick(func(*application.Context) { show("/") })
	enabled, _ := app.Autostart.IsEnabled()
	menu.AddCheckbox("Start at login", enabled).OnClick(func(clicked *application.Context) {
		item := clicked.ClickedMenuItem()
		var err error
		if item.Checked() {
			err = app.Autostart.EnableWithOptions(application.AutostartOptions{Identifier: appIdentifier, Arguments: []string{backgroundFlag}})
		} else {
			err = app.Autostart.Disable()
		}
		if err != nil {
			item.SetChecked(!item.Checked())
			fmt.Fprintf(logFile, "rein app: start at login: %v\n", err)
		}
	})
	menu.AddSeparator()
	menu.Add("Quit rein").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	notifier.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			return
		}
		page, _ := result.Response.UserInfo["page"].(string)
		show(page)
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		_, _ = notifier.RequestNotificationAuthorization()
		_ = notifier.RegisterNotificationCategory(notifications.NotificationCategory{ID: openCategory, Actions: []notifications.NotificationAction{{ID: "open", Title: "Open"}}})
	})
	go func() {
		defer close(owned)
		ownMachine(home, identity, server, logFile)
	}()
	err = app.Run()
	if err != nil {
		fmt.Fprintf(logFile, "rein app: %v\n", err)
		return cli.ExitCrashed
	}
	return cli.ExitWorked
}

// ownMachine makes the app the background process of home: a rein owner
// already there is asked to make way, and the app takes the socket once it
// has; an app of another version that stays there is left alone.
func ownMachine(home string, identity string, server *owner.Server, log io.Writer) {
	socket := owner.SocketPath(home)
	for {
		listener, err := owner.Listen(socket)
		if err == nil {
			err = server.Serve(listener)
			if err != nil {
				fmt.Fprintf(log, "rein app: %v\n", err)
			}
			return
		}
		if !errors.Is(err, owner.ErrRunning) {
			fmt.Fprintf(log, "rein app: the background process did not start: %v\n", err)
			return
		}
		stays, err := owner.Supersede(socket, identity)
		if err == nil && stays {
			fmt.Fprintf(log, "rein app: another rein app is the background process of %s; this one shows the pages only\n", home)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// trayIcon is a ring, drawn in black on clear so macOS can tint it as a
// template for the menu bar.
func trayIcon() []byte {
	const size = 44
	picture := image.NewNRGBA(image.Rect(0, 0, size, size))
	centre := float64(size-1) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			distance := math.Hypot(float64(x)-centre, float64(y)-centre)
			if (distance > 13 && distance < 19) || distance < 5 {
				picture.Set(x, y, color.NRGBA{A: 255})
			}
		}
	}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, picture)
	return encoded.Bytes()
}
