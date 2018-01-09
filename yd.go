package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/slytomcat/confJSON"
	"github.com/slytomcat/systray"
	"github.com/slytomcat/yd-go/icons"
	"github.com/slytomcat/yd-go/ydisk"
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	// Prepare the application configuration
	// Make default app configuration values
	AppCfg := map[string]interface{}{
		"Conf":          expandHome("~/.config/yandex-disk/config.cfg"), // path to daemon config file
		"Theme":         "dark",                                         // icons theme name
		"Notifications": true,                                           // display desktop notification
		"StartDaemon":   true,                                           // start daemon on app start
		"StopDaemon":    false,                                          // stop daemon on app closure
	}
	// Check that app configuration file path exists
	AppConfigHome := expandHome("~/.config/yd-go")
	if notExists(AppConfigHome) {
		err := os.MkdirAll(AppConfigHome, 0766)
		if err != nil {
			log.Fatal("Can't create application configuration path:", err)
		}
	}
	if len(AppConfigFile) == 0 {
		AppConfigFile = filepath.Join(AppConfigHome, "default.cfg")
	} else {
		AppConfigFile = expandHome(AppConfigFile)
	}
	log.Println("Configuration:", AppConfigFile)
	// Check that app configuration file exists
	if notExists(AppConfigFile) {
		//Create and save new configuration file with default values
		confJSON.Save(AppConfigFile, AppCfg)
	} else {
		// Read app configuration file
		confJSON.Load(AppConfigFile, &AppCfg)
	}
	// Check that daemon installed and configured
	FolderPath := checkDaemon(AppCfg["Conf"].(string))
	// Initialize icon theme
	icons.SetTheme("/usr/share/yd-go", AppCfg["Theme"].(string))
	// Initialize systray icon
	systray.SetIcon(icons.IconPause)
	systray.SetTitle("")
	// Initialize systray menu
	mStatus := systray.AddMenuItem("Status: unknown", "")
	mStatus.Disable()
	mSize1 := systray.AddMenuItem("Used: .../...", "")
	mSize1.Disable()
	mSize2 := systray.AddMenuItem("Free: ... Trash: ...", "")
	mSize2.Disable()
	systray.AddSeparator()
	// use 2 ZERO WIDTH SPACES to avoid matching with filenames
	mLast := systray.AddMenuItem("\u200B\u2060Last synchronized", "")
	mLast.Disable()
	systray.AddSeparator()
	mStartStop := systray.AddMenuItem("", "") // no title at start as current status is unknown
	systray.AddSeparator()
	mOutput := systray.AddMenuItem("Show daemon output", "")
	mPath := systray.AddMenuItem("Open: "+FolderPath, "")
	mSite := systray.AddMenuItem("Open YandexDisk in browser", "")
	systray.AddSeparator()
	mHelp := systray.AddMenuItem("Help", "")
	mAbout := systray.AddMenuItem("About", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")
	/*TO_DO:
	 * Additional menu items:
	 * 1. About ??? -> short text within the notification
	 * 2. Help -> redirect to github wiki page "FAQ and how to report issue"
	 * */
	// Create new ydisk interface
	YD := ydisk.NewYDisk(AppCfg["Conf"].(string), FolderPath)
	// Dictionary for last synchronized title (as shorten path) and path (as is)
	var Last map[string]string
	// it have to be protected as it is updated and read from 2 different goroutines
	var LastLock sync.RWMutex
	// Start daemon if it is configured
	if AppCfg["StartDaemon"].(bool) {
		go YD.Start()
	}
	go func() {
		log.Println("Menu handler started")
		defer log.Println("Menu handler exited.")
		// defer request for exit from systray main loop (gtk.main())
		defer systray.Quit()
		for {
			select {
			case title := <-mStartStop.ClickedCh:
				switch title {
				case "Start":
					go YD.Start()
				case "Stop":
					go YD.Stop()
				} // do nothing in other cases
			case title := <-mLast.ClickedCh:
				if !strings.HasPrefix(title, "\u200B\u2060") {
					LastLock.RLock()
					xdgOpen(filepath.Join(FolderPath, Last[title]))
					LastLock.RUnlock()
				}
			case <-mOutput.ClickedCh:
				notifySend(icons.IconNotify, "Yandex.Disk daemon output", YD.Output())
			case <-mPath.ClickedCh:
				xdgOpen(FolderPath)
			case <-mSite.ClickedCh:
				xdgOpen("https://disk.yandex.com")
			case <-mHelp.ClickedCh:
				xdgOpen("https://github.com/slytomcat/YD.go/wiki/FAQ&SUPPORT")
			case <-mAbout.ClickedCh:
				notifySend(icons.IconNotify, " ",
					`yd-go is the panel indicator of Yandex.Disk daemon.

			Version: Betta 0.1

Copyleft 2017-2018 Sly_tom_cat (slytomcat@mail.ru)

			License: GPL v.3
`)
			case <-mQuit.ClickedCh:
				log.Println("Exit requested.")
				// Stop daemon if it is configured
				if AppCfg["StopDaemon"].(bool) {
					YD.Stop()
				}
				YD.Close() // it closes Changes channel
				return
			}
		}
	}()

	go func() {
		log.Println("Changes handler started")
		defer log.Println("Changes handler exited.")
		// Prepare the staff for icon animation
		currentIcon := 0
		tick := time.NewTimer(333 * time.Millisecond)
		defer tick.Stop()
		currentStatus := ""
		for {
			select {
			case yds, ok := <-YD.Changes: // YD changed status event
				if !ok { // as Changes channel closed - exit
					return
				}
				currentStatus = yds.Stat

				mStatus.SetTitle("Status: " + yds.Stat + " " + yds.Prog +
					yds.Err + " " + shortName(yds.ErrP, 30))
				mSize1.SetTitle("Used: " + yds.Used + "/" + yds.Total)
				mSize2.SetTitle("Free: " + yds.Free + " Trash: " + yds.Trash)
				// handle last synchronized submenu
				if yds.ChLast {
					mLast.RemoveSubmenu()
					LastLock.Lock()
					Last = make(map[string]string, 10)
					LastLock.Unlock()
					if len(yds.Last) > 0 {
						for _, p := range yds.Last {
							short := shortName(p, 40)
							mLast.AddSubmenuItem(short, !notExists(p))
							LastLock.Lock()
							Last[short] = p
							LastLock.Unlock()
						}
						mLast.Enable()
					} else {
						mLast.Disable()
					}
				}
				//
				if yds.Stat != yds.Prev { // status changed
					// change indicator icon
					switch yds.Stat {
					case "idle":
						systray.SetIcon(icons.IconIdle)
					case "busy", "index":
						systray.SetIcon(icons.IconBusy[currentIcon])
						tick.Reset(333 * time.Millisecond)
					case "none", "paused":
						systray.SetIcon(icons.IconPause)
					default:
						systray.SetIcon(icons.IconError)
					}
					// handle Start/Stop menu title
					if yds.Stat == "none" {
						mStartStop.SetTitle("Start")
					} else if mStartStop.GetTitle() != "Stop" {
						mStartStop.SetTitle("Stop")
					}
					// handle notifications
					if AppCfg["Notifications"].(bool) {
						switch {
						case yds.Stat == "none" && yds.Prev != "unknown":
							notifySend(icons.IconNotify, "Yandex.Disk", "Daemon stopped")
						case yds.Prev == "none":
							notifySend(icons.IconNotify, "Yandex.Disk", "Daemon started")
						case (yds.Stat == "busy" || yds.Stat == "index") &&
							(yds.Prev != "busy" && yds.Prev != "index"):
							notifySend(icons.IconNotify, "Yandex.Disk", "Synchronization started")
						case (yds.Stat == "idle" || yds.Stat == "error") &&
							(yds.Prev == "busy" || yds.Prev == "index"):
							notifySend(icons.IconNotify, "Yandex.Disk", "Synchronization finished")
						}
					}
				}
				log.Println("Change handled")
			case <-tick.C: //  timer event
				currentIcon++
				currentIcon %= 5
				if currentStatus == "busy" || currentStatus == "index" {
					systray.SetIcon(icons.IconBusy[currentIcon])
					tick.Reset(333 * time.Millisecond)
				}
			}
		}
	}()
}

func onExit() {}
