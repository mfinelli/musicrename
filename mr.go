package main

import "fmt"
import "os"
import "time"

import "github.com/pborman/getopt"

import "github.com/mfinelli/musicrename/config"
import "github.com/mfinelli/musicrename/walk"

const VERSION = "0.0.1"

func main() {
	start := time.Now()

	versionFlag := getopt.BoolLong("version", 'v', "print version")
	helpFlag := getopt.BoolLong("help", 'h', "print help")
	dryRunFlag := getopt.BoolLong("dry-run", 'n', "don't move or rename")
	verboseFlag := getopt.BoolLong("verbose", 'd', "extra output")
	getopt.Parse()

	if *helpFlag {
		getopt.Usage()
		os.Exit(0)
	}

	if *versionFlag {
		fmt.Printf("musicrename v%s\n", VERSION)
		os.Exit(0)
	}

	dryRun := *dryRunFlag
	verbose := *verboseFlag

	if verbose {
		fmt.Println("Running in verbose mode")
	}

	if dryRun {
		fmt.Println("Running in dry-run mode")
	}

	conf, _ := config.ReadOrCreateConfigFile()
	args := getopt.Args()
	var workdir string

	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "too many arguments")
		os.Exit(1)
	} else if len(args) == 1 {
		if _, err := os.Stat(args[0]); err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			} else {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		workdir = args[0]
	} else {
		workdir, _ = os.Getwd()
	}

	fmt.Printf("doing work in: %s\n", workdir)

	counts := walk.WalkAndProcessDirectory(verbose, dryRun, workdir, conf)

	end := time.Now()
	fmt.Printf("Processed %d directories and %d files in %v.\n", counts[0], counts[1], end.Sub(start))
}
