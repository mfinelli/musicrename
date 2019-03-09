package main

import "fmt"
import "os"
import "github.com/pborman/getopt"

const VERSION = "0.0.1"

func main() {
	versionFlag := getopt.BoolLong("version", 'v', "print version")
	helpFlag := getopt.BoolLong("help", 'h', "print help")
	getopt.Parse()

	if *helpFlag {
		getopt.Usage()
		os.Exit(0)
	}

	if *versionFlag {
		fmt.Printf("musicrename v%s\n", VERSION)
		os.Exit(0)
	}
}
