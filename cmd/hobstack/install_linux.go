//go:build linux

package main

import (
	"bufio"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hobstack/internal/config"
)

// `hobstack install` / `hobstack uninstall` pe Linux: echivalentul installerului de Windows. Copiaza binarul in
// folderul ales (implicit ~/.local/share/hobstack), pune marcajul pentru datele langa exe (<folder>/data),
// intrarea din meniul de aplicatii, iconita de pe Desktop si ~/.local/bin/hobstack pentru terminal.

//go:embed appicon.png
var appIcon []byte

func xdgData(home string) string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	return filepath.Join(home, ".local", "share")
}

// desktopDir: folderul Desktop al utilizatorului (xdg-user-dir, altfel ~/Desktop); "" daca nu exista.
func desktopDir(home string) string {
	if out, err := exec.Command("xdg-user-dir", "DESKTOP").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" && d != home {
			return d
		}
	}
	d := filepath.Join(home, "Desktop")
	if info, err := os.Stat(d); err == nil && info.IsDir() {
		return d
	}
	return ""
}

func launcher(exe, icon string) string {
	return "[Desktop Entry]\nType=Application\nName=HobStack\nComment=Local Claude Code agent teams\n" +
		"Exec=\"" + exe + "\" start\nIcon=" + icon + "\nTerminal=false\nCategories=Development;\nStartupNotify=false\n"
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, to) // rename: merge si peste un binar care ruleaza
}

func install(args []string) int {
	fl := flag.NewFlagSet("install", flag.ExitOnError)
	dirFlag := fl.String("dir", "", "where to install (default ~/.local/share/hobstack)")
	yes := fl.Bool("y", false, "don't ask, use the defaults")
	fl.Parse(args)
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	dir := *dirFlag
	if dir == "" {
		dir = filepath.Join(xdgData(home), "hobstack")
		if !*yes {
			fmt.Printf("Install HobStack to [%s]: ", dir)
			if line, _ := bufio.NewReader(os.Stdin).ReadString('\n'); strings.TrimSpace(line) != "" {
				dir = strings.TrimSpace(line)
			}
		}
	}
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(home, dir[2:])
	}
	dir, _ = filepath.Abs(dir)
	if err := doInstall(home, dir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("HobStack is installed in %s (your data goes in %s).\n", dir, filepath.Join(dir, "data"))
	fmt.Println("Open it from your applications menu or the desktop icon; it also starts when you log in.")
	// pornim versiunea instalata in fundal (o versiune care ruleaza deja e oprita de doInstall)
	cmd := exec.Command(filepath.Join(dir, "hobstack"), "start")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err == nil {
		cmd.Process.Release()
	}
	return 0
}

func doInstall(home, dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(dir, "hobstack")
	// HobStack-ul care ruleaza (oricare, dupa port) se opreste inainte, ca datele sa se poata muta
	if code := quit(nil); code == 2 {
		fmt.Print("HobStack agents are working right now. Stop them and continue? [y/N]: ")
		if line, _ := bufio.NewReader(os.Stdin).ReadString('\n'); !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
			return errors.New("cancelled; nothing changed")
		}
		quit([]string{"--force"})
	}
	if filepath.Clean(self) != filepath.Clean(exe) {
		if err := copyFile(self, exe, 0o755); err != nil {
			return fmt.Errorf("copying hobstack: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, config.InstallMarker), []byte("HobStack keeps its data in the data folder next to this file.\n"), 0o644); err != nil {
		return err
	}
	icon := filepath.Join(dir, "hobstack.png")
	if err := os.WriteFile(icon, appIcon, 0o644); err != nil {
		return err
	}
	entry := launcher(exe, icon)
	apps := filepath.Join(xdgData(home), "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(apps, "hobstack.desktop"), []byte(entry), 0o755); err != nil {
		return err
	}
	if desk := desktopDir(home); desk != "" {
		p := filepath.Join(desk, "hobstack.desktop")
		if err := os.WriteFile(p, []byte(entry), 0o755); err == nil {
			// GNOME porneste iconitele de pe Desktop doar daca sunt marcate de incredere
			exec.Command("gio", "set", p, "metadata::trusted", "true").Run()
		}
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err == nil {
		link := filepath.Join(bin, "hobstack")
		if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			os.Remove(link)
		}
		if _, err := os.Lstat(link); os.IsNotExist(err) {
			os.Symlink(exe, link)
		}
	}
	exec.Command("update-desktop-database", apps).Run()
	return nil
}

func uninstall(args []string) int {
	fl := flag.NewFlagSet("uninstall", flag.ExitOnError)
	deleteData := fl.Bool("delete-data", false, "also delete projects, tasks and history (the data folder)")
	fl.Parse(args)
	home, _ := os.UserHomeDir()
	dir := config.InstallDir()
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: this HobStack was not installed with `hobstack install`")
		return 1
	}
	if code := quit(nil); code == 2 {
		fmt.Print("HobStack agents are working right now. Stop them and uninstall? [y/N]: ")
		if line, _ := bufio.NewReader(os.Stdin).ReadString('\n'); !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
			fmt.Println("Cancelled.")
			return 1
		}
		quit([]string{"--force"})
	}
	os.Remove(filepath.Join(xdgData(home), "applications", "hobstack.desktop"))
	if desk := desktopDir(home); desk != "" {
		os.Remove(filepath.Join(desk, "hobstack.desktop"))
	}
	if target, err := os.Readlink(filepath.Join(home, ".local", "bin", "hobstack")); err == nil && strings.HasPrefix(target, dir) {
		os.Remove(filepath.Join(home, ".local", "bin", "hobstack"))
	}
	autostartFile := filepath.Join(home, ".config", "autostart", "hobstack.desktop")
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		autostartFile = filepath.Join(x, "autostart", "hobstack.desktop")
	}
	os.Remove(autostartFile)
	for _, f := range []string{"hobstack", "hobstack.prev", "hobstack.new", "hobstack.png", config.InstallMarker} {
		os.Remove(filepath.Join(dir, f))
	}
	if *deleteData {
		os.RemoveAll(filepath.Join(dir, "data"))
	} else if _, err := os.Stat(filepath.Join(dir, "data")); err == nil {
		fmt.Printf("Your data stays in %s (delete it with --delete-data, or install again in %s to keep using it).\n", filepath.Join(dir, "data"), dir)
	}
	os.Remove(dir) // doar daca e gol
	fmt.Println("HobStack is uninstalled.")
	return 0
}
