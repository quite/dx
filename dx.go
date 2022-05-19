package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	docker "github.com/fsouza/go-dockerclient"

	"golang.org/x/term"
)

func main() {
	psCommand := flag.NewFlagSet("ps", flag.ExitOnError)
	psallFlag := psCommand.Bool("a", false, "show all containers")
	iCommand := flag.NewFlagSet("i", flag.ExitOnError)
	iallFlag := iCommand.Bool("a", false, "show all images")

	if len(os.Args) == 1 {
		fmt.Println("subcommands:")
		fmt.Println("  ps")
		fmt.Println("  i|imgs|images")
		return
	}
	switch os.Args[1] {
	case "ps":
		psCommand.Parse(os.Args[2:])
	case "i", "imgs", "images":
		iCommand.Parse(os.Args[2:])
	default:
		fmt.Printf("%q: unknown subcommand.\n", os.Args[1])
		os.Exit(2)
	}

	endpoint := "unix:///var/run/docker.sock"
	if dockerhost := os.Getenv("DOCKER_HOST"); dockerhost != "" {
		endpoint = dockerhost
	}

	client, err := docker.NewClient(endpoint)
	if err != nil {
		log.Fatalf("NewClient: %s", err)
	}

	if psCommand.Parsed() {
		ps(client, *psallFlag)
	}
	if iCommand.Parsed() {
		imgs(client, *iallFlag)
	}

}

func ps(client *docker.Client, all bool) {
	containers, err := client.ListContainers(
		docker.ListContainersOptions{
			All: all, Size: false,
		})
	if err != nil {
		log.Fatalf("ListContainers: %s", err)
	}

	width := float64(termwidth())

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 2, 1, ' ', 0)
	if width > 100 {
		fmt.Fprintln(w, "id\tname\tup\tip\tports\tcmd\tage\timage")
	} else {
		fmt.Fprintln(w, "id\tname\tup\tip\tports\tage\timage")
	}
	for _, c := range containers {
		cinfo, err := client.InspectContainerWithOptions(
			docker.InspectContainerOptions{
				ID: c.ID,
			})
		if err != nil {
			log.Fatalf("InspectContainer: %s", err)
		}
		line := c.ID[:6]

		line += "\t" + shorten(strings.TrimPrefix(cinfo.Name, "/"), int(0.2*width))

		line += "\t" + state(cinfo.State)

		// TODO, only one IP?
		ips := ips(c.Networks)
		line += "\t" + ips[0]

		line += "\t"
		portlines := ports(c.Ports)
		if len(portlines) > 0 {
			line += portlines[0]
		}

		if width > 100 {
			line += "\t" + shorten(c.Command, int(0.15*width))
		}

		line += "\t" + prettyDuration(time.Since(time.Unix(c.Created, 0)))

		line += "\t" + shorten(c.Image, int(0.2*width))

		fmt.Fprintln(w, line)

		if len(portlines) >= 2 {
			for _, l := range portlines[1:] {
				fmt.Fprintf(w, " \t \t \t \t%s\n", l)
			}
		}
	}
	w.Flush()
}

func imgs(client *docker.Client, all bool) {
	imgs, err := client.ListImages(
		docker.ListImagesOptions{
			All: all,
		})
	if err != nil {
		log.Fatalf("ListImages: %s", err)
	}

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "id\tage\tsize\trepotags")
	for _, i := range imgs {
		id := i.ID
		if strings.ContainsAny(i.ID, ":") {
			id = strings.SplitN(i.ID, ":", 2)[1]
		}
		line := id[:6]
		line += "\t" + prettyDuration(time.Since(time.Unix(i.Created, 0)))
		line += "\t" + shortenBytes(i.Size)
		line += "\t" + strings.Join(i.RepoTags, " ")
		fmt.Fprintln(w, line)
	}
	w.Flush()
}

func termwidth() int {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return 999
	}
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatalf("terminal.GetSize: %s", err)
	}
	return width
}

func state(state docker.State) string {
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
			buf.WriteString("restart")
		}
		buf.WriteString(fmt.Sprintf("(%d)%s", state.ExitCode, prettyDuration(time.Since(state.FinishedAt))))
		return buf.String()
	}
	buf.WriteString(prettyDuration(time.Since(state.StartedAt)))
	if state.Paused {
		buf.WriteString("Paused")
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

func ips(networklist docker.NetworkList) []string {
	s := []string{}
	for _, cnetwork := range networklist.Networks {
		s = append(s, cnetwork.IPAddress)
	}
	return s
}

func ports(ports []docker.APIPort) []string {
	lines := []string{}
	for _, p := range ports {
		line := ""
		if p.Type == "udp" {
			line += p.Type + ":"
		}
		if p.IP != "" {
			line += strconv.FormatInt(p.PublicPort, 10) + "→"
		}
		line += strconv.FormatInt(p.PrivatePort, 10)
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

func shortenBytes(bytes int64) string {
	byts := float64(bytes)
	unit := float64(1024)
	if byts < unit {
		return fmt.Sprintf("%d", bytes)
	}
	exp := math.Log(byts) / math.Log(unit)
	return fmt.Sprintf("%.1f %cB",
		byts/math.Pow(unit, math.Floor(exp)),
		"kMGTPE"[int(exp)-1])
}
