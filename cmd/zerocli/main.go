package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	zcClient "github.com/jasonrowsell/zerocache/pkg/client"
)

var (
	host = flag.String("h", "127.0.0.1", "Server host")
	port = flag.String("p", "6380", "Server port")
)

func main() {
	flag.Parse()

	serverAddr := fmt.Sprintf("%s:%s", *host, *port)

	args := flag.Args()
	if len(args) > 0 {
		runNonInteractiveMode(serverAddr, args)
	} else {
		runInteractiveMode(serverAddr)
	}
}

func runNonInteractiveMode(addr string, args []string) {
	cli, err := zcClient.New(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer cli.Close()

	output, err := executeCommand(cli, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "(error) %v\n", err)
		os.Exit(1)
	}
	fmt.Println(output)
}

// runInteractiveMode starts the Read-Eval-Print Loop.
func runInteractiveMode(addr string) {
	cli, err := zcClient.New(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer cli.Close()

	fmt.Printf("Connected to ZeroCache at %s\n", addr)
	fmt.Println("Type commands (e.g., SET key value, GET key, DEL key, QUIT)")

	reader := bufio.NewReader(os.Stdin)

	for {
		// Prompt
		fmt.Printf("%s> ", addr)

		// Read input line
		input, err := reader.ReadString('\n')
		if err != nil {
			// Handle EOF (Ctrl+D) gracefully
			if err.Error() == "EOF" {
				fmt.Println()
				return
			}
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return
		}

		// Trim whitespace
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Split input into command and arguments
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		commandUpper := strings.ToUpper(parts[0])

		// Handle client-side commands
		if commandUpper == "QUIT" || commandUpper == "EXIT" {
			fmt.Println("Exiting.")
			return
		}
		if commandUpper == "HELP" {
			printHelp()
			continue
		}

		output, err := executeCommand(cli, parts)
		if err != nil {
			fmt.Printf("(error) %v\n", err)
		} else {
			fmt.Println(output)
		}
	}
}

// executeCommand takes the command parts, calls the appropriate client method,
// and formats the output string or returns an error.
func executeCommand(cli *zcClient.Client, parts []string) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("no command provided")
	}

	command := strings.ToUpper(parts[0])
	args := parts[1:]

	switch command {
	case "SET":
		if len(args) != 2 {
			return "", fmt.Errorf("ERR wrong number of arguments for 'SET' command (usage: SET key value)")
		}
		key := args[0]
		value := []byte(args[1]) // Value is treated as raw string for now
		err := cli.Set(key, value)
		if err != nil {
			return "", err
		}
		return "OK", nil

	case "GET":
		if len(args) != 1 {
			return "", fmt.Errorf("ERR wrong number of arguments for 'GET' command (usage: GET key)")
		}
		key := args[0]
		value, err := cli.Get(key)
		if err != nil {
			if err == zcClient.ErrNotFound {
				return "(nil)", nil
			}
			return "", err
		}
		return fmt.Sprintf("%q", string(value)), nil

	case "DEL", "DELETE":
		if len(args) != 1 {
			return "", fmt.Errorf("ERR wrong number of arguments for 'DEL' command (usage: DEL key)")
		}
		key := args[0]
		err := cli.Delete(key)
		if err != nil {
			return "", err
		}
		return "OK", nil

	case "PING":
		if len(args) > 1 {
			return "", fmt.Errorf("ERR wrong number of arguments for 'PING' command")
		}
		if len(args) == 0 {
			return "\"PONG\"", nil
		}
		return fmt.Sprintf("%q", args[0]), nil

	default:
		return "", fmt.Errorf("ERR unknown command '%s'", parts[0])
	}
}

// printHelp displays basic usage instructions.
func printHelp() {
	fmt.Println("ZeroCache CLI Help:")
	fmt.Println("  SET <key> <value>   - Set key to hold the string value.")
	fmt.Println("  GET <key>           - Get the value of key.")
	fmt.Println("  DEL <key>           - Delete a key.")
	fmt.Println("  HELP                - Show this help message.")
	fmt.Println("  QUIT / EXIT         - Disconnect and exit the CLI.")
}
