// Command samcast-tray is the samsung-cast tray helper: one StatusNotifierItem
// icon in the waybar/freedesktop tray whose pixmap and tooltip mirror the
// mirroring state, with a menu to open the control panel and start/stop
// mirroring while the panel window is closed.
//
// Started by `samcast tray` (samsung-cast-tray.service) and polls
// doubletake-ctl every --interval to keep icon and menu current.
//
// SECURITY MODEL: the item exposes only start/stop mirroring plus an
// open-panel action, and only on the local session bus — the same
// loopback-style trust as samcast-ui: anything that can reach us on the
// session bus already runs as this user, which is exactly what the panel and
// the samcast CLI would allow anyway. No config rewrite or log access is
// exposed through the tray.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

func main() {
	log.SetFlags(log.Ltime)

	ctlFlag := flag.String("ctl", "", "path to doubletake-ctl (default: <dir of executable>/../bin/doubletake-ctl)")
	samcastFlag := flag.String("samcast", "", "path to the samcast CLI (default: <dir of executable>/../samcast)")
	interval := flag.Duration("interval", 2*time.Second, "state poll interval")
	id := flag.String("id", "samcast", "tray item id")
	flag.Parse()

	ctl := resolvePath(*ctlFlag, filepath.Join("bin", "doubletake-ctl"))
	samcastPath := resolvePath(*samcastFlag, "samcast")
	for _, pc := range []struct{ label, flag, path string }{
		{"samcast", "samcast", samcastPath},
		{"doubletake-ctl", "ctl", ctl},
	} {
		if pc.path == "" {
			log.Printf("warning: %s path could not be resolved (pass --%s)", pc.label, pc.flag)
			continue
		}
		if fi, err := os.Stat(pc.path); err != nil || fi.IsDir() {
			log.Printf("warning: %s not found at %s — related actions will fail until the path is fixed", pc.label, pc.path)
		}
	}
	acts := &actions{samcast: samcastPath, samcastOK: samcastPath != "" && isFile(samcastPath)}
	if !acts.samcastOK {
		log.Printf("samcast missing — tray actions disabled (Quit still works)")
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		log.Fatalf("cannot connect to the session bus: %v", err)
	}
	// RequestNameReplaceExisting: a just-killed previous instance keeps the
	// name until the bus notices, and restart races are normal.
	// RequestNameDoNotQueue: fail fast rather than queue behind someone else.
	busName := fmt.Sprintf("%s-%d-1", sniBusPrefix, os.Getpid())
	if _, err := conn.RequestName(busName, dbus.NameFlagReplaceExisting|dbus.NameFlagDoNotQueue); err != nil {
		log.Fatalf("cannot request %s: %v", busName, err)
	}

	state := &stateStore{}
	menu := &dbusMenu{state: state, acts: acts}
	srv := &trayServer{
		conn:    conn,
		busName: busName,
		id:      *id,
		state:   state,
		icons:   &iconCache{},
		menu:    menu,
		acts:    acts,
	}
	if err := srv.export(); err != nil {
		log.Fatalf("export: %v", err)
	}

	sigCh := make(chan *dbus.Signal, 8)
	conn.Signal(sigCh)
	// Watch the watcher: a waybar restart drops our registration and the new
	// owner must see a fresh RegisterStatusNotifierItem call.
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherBus),
	); err != nil {
		log.Fatalf("cannot watch %s: %v", watcherBus, err)
	}

	// One poll before registering so the first GetAll already carries real
	// state; then the loops take over.
	srv.applyState(pollState(ctl))
	go srv.runRegistration(sigCh)
	go pollLoop(ctl, *interval, srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		// SIGTERM (systemd stop) exits 0 so Restart=on-failure stays quiet.
		log.Printf("terminated, exiting")
		os.Exit(0)
	}()

	select {} // the goroutines own everything from here
}

// pollLoop refreshes the rendered state every interval. A panic in one
// iteration is logged and swallowed — the tray outlives everything.
func pollLoop(ctl string, interval time.Duration, srv *trayServer) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for range tick.C {
		func() {
			defer func() {
				if v := recover(); v != nil {
					log.Printf("panic in poll loop: %v", v)
				}
			}()
			srv.applyState(pollState(ctl))
		}()
	}
}

// resolvePath returns flagPath when set, otherwise flagPath resolved relative
// to the executable: <exe dir>/../<rel>. Same layout rule as samcast-ui.
func resolvePath(flagPath, rel string) string {
	if flagPath != "" {
		return flagPath
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "..", rel)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// actions runs samcast subcommands detached from the tray.
type actions struct {
	samcast   string
	samcastOK bool
}

func (a *actions) run(args ...string) {
	if !a.samcastOK {
		log.Printf("cannot run samcast %s: binary missing", strings.Join(args, " "))
		return
	}
	// Detached: stdin is the null device and output is discarded, so samcast's
	// TTY-guarded prompts can never fire and nothing outlives the tray's stdio.
	cmd := exec.Command(a.samcast, args...)
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		log.Printf("samcast %s: %v", strings.Join(args, " "), err)
		return
	}
	// Wait off-thread so exited children don't linger as zombies.
	go func() { _ = cmd.Wait() }()
}

func (a *actions) open()  { a.run("ui") }
func (a *actions) start() { a.run("on") }
func (a *actions) stop()  { a.run("off") }
