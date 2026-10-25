package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"axamp/internal/config"
	"axamp/internal/dbmanager"
	"axamp/internal/generator"
	"axamp/internal/profiler"
	"axamp/internal/server"
	"axamp/internal/supervisor"
	"axamp/internal/winutil"
)

func main() {
	minimized := flag.Bool("minimized", false, "Start silently in background without opening GUI window")
	headless := flag.Bool("headless", false, "Run in console/daemon mode without launching browser/app window")
	flag.Parse()

	rootDir, err := os.Getwd()
	if err != nil {
		exe, _ := os.Executable()
		rootDir = filepath.Dir(exe)
	}

	fmt.Println("====================================================")
	fmt.Println("   AXAMP v2.0.0 — Smart Portable Server Engine      ")
	fmt.Println("   مبتكر النظام: عدنان سلطان حسن                    ")
	fmt.Println("   Creator: Adnan Sultan Hassan <donadnan2002@gmail.com>")
	fmt.Println("====================================================")
	fmt.Println("[*] Root Portable Path:", rootDir)

	// 1. Load or Initialize Configuration
	cfg, err := config.LoadOrInit(rootDir)
	if err != nil {
		fmt.Println("[!] Config warning:", err)
	}

	// 2. Profile System Hardware & Smart Ports
	hw := profiler.InspectSystem()
	fmt.Printf("[*] Hardware Profile: %d CPU Cores | %d MB Total RAM (%d%% used)\n",
		hw.CPUCores, hw.TotalRAMMB, hw.MemoryLoadPercent)
	fmt.Printf("[*] Smart Auto-Tune: PHP memory_limit=%s | MySQL innodb_buffer_pool=%s\n",
		hw.RecommendedPHPMemory, hw.RecommendedInnoDBSize)

	if !profiler.IsPortAvailable(cfg.ControlPort) {
		newCtrl, _ := profiler.FindSmartPort(cfg.ControlPort, []string{"4041", "4042", "4080"}, 4050)
		cfg.ControlPort = newCtrl
	}

	// 3. Initialize Process Supervisor & Generate Portable Configs
	sup := supervisor.NewManager(cfg)
	if err := sup.EnsureSmartPortsAndConfigs(); err != nil {
		fmt.Println("[!] Error generating configs:", err)
	} else {
		_ = generator.GenerateAll(cfg, hw)
		fmt.Printf("[+] Dynamic configs generated in %s\\conf (Mode: %s)\n", rootDir, cfg.Mode)
		fmt.Printf("[+] Assigned Ports -> Apache: %s | MySQL: %s | Control GUI: %s\n",
			cfg.ApachePort, cfg.MySQLPort, cfg.ControlPort)
	}

	// 4. Auto-start Apache & MySQL if configured
	if cfg.AutoStartServices {
		fmt.Println("[*] Auto-starting Apache & MySQL services...")
		sup.StartAll()
	}

	// 4b. Start Auto-Backup Compressed Scheduler (.sql.zip / raw.zip)
	dbmanager.StartAutoBackupScheduler(cfg, func(msg string) {
		fmt.Println("[+] " + msg)
	})

	// 5. Start Control Plane & GUI Server
	ctrlURL := fmt.Sprintf("http://127.0.0.1:%s", cfg.ControlPort)
	fmt.Println("[+] AXAMP Control Center ready at:", ctrlURL)
	fmt.Println("[+] Remote Security PIN:", cfg.RemotePIN)

	srv := server.NewServer(cfg, sup)
	go func() {
		if err := srv.Start(); err != nil {
			fmt.Println("[-] Control server error:", err)
		}
	}()

	// 6. Open Native-like App Window unless --minimized or --headless
	if !*minimized && !*headless {
		go func() {
			time.Sleep(300 * time.Millisecond)
			cmd, err := winutil.OpenAppWindow(ctrlURL)
			if err == nil && cmd != nil && cmd.Process != nil {
				// When user closes the native app window ('X'), automatically shut down all services!
				_ = cmd.Wait()
				fmt.Println("\n[*] App window closed. Shutting down AXAMP managed services...")
				sup.StopAll()
				os.Exit(0)
			}
		}()
	}

	// 7. Graceful Shutdown Handling (Signals / OS events)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n[*] Shutting down AXAMP managed services...")
	sup.StopAll()
	fmt.Println("[+] Goodbye!")
}
