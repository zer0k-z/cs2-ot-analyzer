package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"
)

// Run like this: go run print-ot.go -demo="/path/to/demo.dem"
// Run like this: go run print-ot.go -dir="/path/to/"
type Result struct {
	Path  string
	Error error
}

type Team common.Team

func (t Team) String() string {
	switch common.Team(t) {
	case common.TeamUnassigned:
		return "Unassigned"
	case common.TeamSpectators:
		return "Spectators"
	case common.TeamTerrorists:
		return "Terrorists"
	case common.TeamCounterTerrorists:
		return "Counter-Terrorists"
	default:
		return "Unknown"
	}
}
func main() {

	dir := flag.String("dir", "", "Directory to process")

	demo := flag.String("demo", "", "Demo file `path`")

	max := flag.Int("max-concurrent", 8, "Maximum amount of demos parsed at the same time")

	result := make(chan Result)

	// Parse the flags
	flag.Parse()

	// WaitGroup to wait for all goroutines to finish
	wg := &sync.WaitGroup{}
	semaphore := make(chan struct{}, *max)

	if (*dir == "" && *demo == "") || (*dir != "" && *demo != "") {
		fmt.Println("Error: -dir OR -demo flag is required")
		flag.Usage()
		os.Exit(1)
	}

	if *demo != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()

			parseDemo(*demo, result)
		}()

	} else {
		fmt.Println("Parsing dir", *dir)

		err := filepath.Walk(*dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && filepath.Ext(info.Name()) == ".dem" {
				fmt.Println("Parsing demo file:", path)
				wg.Add(1)
				go func(path string) {
					defer wg.Done()
					defer func() { <-semaphore }() // Release the semaphore
					semaphore <- struct{}{}        // Acquire semaphore
					parseDemo(path, result)
				}(path)
			}

			return nil
		})
		checkError(err)
	}

	go func() {
		wg.Wait()
		close(result)
	}()

	for res := range result {
		if res.Error != nil {
			fmt.Printf("Failed goroutines: Path=%s, Error: %v\n", res.Path, res.Error)
		}
	}

	fmt.Println("Parsing done.")
}

type OTWin struct {
	numOT  int
	winner Team
}

func parseDemo(path string, result chan<- Result) {
	reported := false
	outputPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".csv"
	// Create array
	otWins := make([]OTWin, 0)
	var res Result
	res.Path = path
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("panic occurred for path %s: %s\n", path, err)
			os.Remove(outputPath)
			res.Error = fmt.Errorf("panic occurred: %v", err)
			result <- res
		}
	}()

	output, err := os.Create(outputPath)
	checkError(err)

	defer output.Close()

	f, err := os.Open(path)
	checkError(err)

	defer f.Close()

	stat, err := f.Stat()
	checkError(err)

	p := demoinfocs.NewParser(f)
	defer p.Close()
	p.RegisterEventHandler(func(e events.RoundEnd) {
		if p.GameState().OvertimeCount() < 1 {
			return
		}
		otWins = append(otWins, OTWin{numOT: p.GameState().OvertimeCount(), winner: Team(e.Winner)})

	})
	spewReport := func() {
		if reported {
			return
		}
		output.WriteString("Date,Map,Overtime,Winner\n")
		for _, win := range otWins {
			output.WriteString(fmt.Sprintf("%s,%s,%d,%s\n", stat.ModTime().Format(time.DateTime), p.Header().MapName, win.numOT, win.winner))
		}
		reported = true
	}
	p.RegisterEventHandler(func(events.AnnouncementWinPanelMatch) {
		spewReport()
	})
	// Parse to end
	err = p.ParseToEnd()
	spewReport()
	fmt.Println("Parsing done for", path)
	checkError(err)

}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}
