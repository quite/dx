package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/samalba/dockerclient"
)

func main() {
	psCommand := flag.NewFlagSet("ps", flag.ExitOnError)
	allFlag := psCommand.Bool("a", false, "show all containers")

	if len(os.Args) == 1 {
		fmt.Println("subcommands:")
		fmt.Println("  ps")
		return
	}
	switch os.Args[1] {
	case "ps":
		psCommand.Parse(os.Args[2:])
	default:
		fmt.Printf("%q: unknown subcommand.\n", os.Args[1])
		os.Exit(2)
	}

	sock := "unix:///var/run/docker.sock"
	if dockerhost := os.Getenv("DOCKER_HOST"); dockerhost != "" {
		sock = dockerhost
	}

	dc, err := dockerclient.NewDockerClient(sock, nil)
	if err != nil {
		log.Fatal(fmt.Errorf("NewDockerClient: %s", err))
	}

	if psCommand.Parsed() {
		ps(dc, *allFlag)
	}

}

func ps(dc *dockerclient.DockerClient, all bool) {
	// all bool, size bool, filters string
	containers, err := dc.ListContainers(all, false, "")
	if err != nil {
		log.Fatal(fmt.Errorf("ListContainers: %s", err))
	}

	width := float64(termwidth())

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 2, 1, ' ', 0)
	if width > 100.0 {
		fmt.Fprintln(w, "id\tname\tup\tip\tports\tcmd\tcre\timage")
	} else {
		fmt.Fprintln(w, "id\tname\tup\tip\tports\tcre\timage")
	}
	for _, c := range containers {
		cinfo, err := dc.InspectContainer(c.Id)
		if err != nil {
			log.Fatal(fmt.Errorf("InspectContainer: %s", err))
		}
		line := c.Id[:5] + "\t"

		line += shorten(strings.TrimPrefix(cinfo.Name, "/"), int(0.2*width)) + "\t"

		line += fmt.Sprintf("%s", state(cinfo.State)) + "\t"

		// TODO, only one IP?
		ips := ips(c.NetworkSettings.Networks)
		line += ips[0] + "\t"

		portlines := ports(c.Ports)
		if len(portlines) > 0 {
			line += portlines[0]
		}
		line += "\t"

		if width > 100 {
			line += shorten(c.Command, int(0.15*width)) + "\t"
		}

		line += fmt.Sprintf("%s", prettyDuration(time.Since(time.Unix(c.Created, 0)))) + "\t"

		line += shorten(c.Image, int(0.2*width)) + "\t"

		fmt.Fprintln(w, line)

		if len(portlines) >= 2 {
			for _, l := range portlines[1:] {
				if width > 100 {
					fmt.Fprintf(w, " \t \t \t \t%s \t \t\n", l)
				} else {
					fmt.Fprintf(w, " \t \t \t \t%s \t\n", l)
				}
			}
		}

	}
	w.Flush()
}

func termwidth() int {
	width, _, err := terminal.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(fmt.Errorf("terminal.GetSize: %s", err))
	}
	return width
}

func state(state *dockerclient.State) string {
	var buf bytes.Buffer
	if !state.Running || state.Restarting {
		if state.Dead {
			return "dead"
		} else if state.StartedAt.IsZero() {
			return "created"
		} else if state.FinishedAt.IsZero() {
			return "FinishedAt==0"
		}
		if !state.Running {
			buf.WriteString("exit")
		} else {
			buf.WriteString("restarting")
		}
		buf.WriteString(fmt.Sprintf("(%d)%s", state.ExitCode, prettyDuration(time.Since(state.FinishedAt))))
		return buf.String()
	}
	buf.WriteString(fmt.Sprintf("%s", prettyDuration(time.Since(state.StartedAt))))
	if state.Paused {
		buf.WriteString(" (paused)")
	}
	return buf.String()
}

func prettyDuration(duration time.Duration) string {
	if seconds := int(duration.Seconds()); seconds < 1 {
		return "now"
	} else if seconds < 60 { // 1m
		return fmt.Sprintf("%ds", seconds)
	} else if minutes := int(duration.Minutes()); minutes < 60 { // 1h
		return fmt.Sprintf("%dm", minutes)
	} else if hours := int(duration.Hours()); hours < 24*3 { // 3d
		return fmt.Sprintf("%dh", hours)
	} else if hours < 24*7*2 { // 2w
		return fmt.Sprintf("%dd", hours/24)
	} else if hours < 24*30*2 { // 2M
		return fmt.Sprintf("%dw", hours/24/7)
	} else if hours < 24*365*2 { // 2y
		return fmt.Sprintf("%dM", hours/24/30)
	}
	return fmt.Sprintf("%dy", int(duration.Hours())/24/365)
}

func ips(es map[string]dockerclient.EndpointSettings) []string {
	s := []string{}
	for _, v := range es {
		s = append(s, v.IPAddress)
	}
	return s
}

func ports(ports []dockerclient.Port) []string {
	lines := []string{}
	for _, p := range ports {
		line := ""
		if p.Type == "udp" {
			line += p.Type + ":"
		}
		if p.IP != "" {
			line += strconv.Itoa(p.PublicPort) + "→"
		}
		line += strconv.Itoa(p.PrivatePort)
		lines = append(lines, line)
	}
	return lines
}

func shorten(s string, l int) string {
	if len(s) <= l {
		return s
	}
	l--
	return s[:l/2+l%2] + "…" + s[len(s)-l/2:]
}
