package main

import (
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
	vCommand := flag.NewFlagSet("v", flag.ExitOnError)

	if len(os.Args) == 1 {
		fmt.Println("subcommands:")
		fmt.Println("  ps")
		fmt.Println("  i|imgs|images")
		fmt.Println("  v|vols|volumes")
		return
	}
	switch os.Args[1] {
	case "ps":
		psCommand.Parse(os.Args[2:])
	case "i", "imgs", "images":
		iCommand.Parse(os.Args[2:])
	case "v", "vols", "volumes":
		vCommand.Parse(os.Args[2:])
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
	if vCommand.Parsed() {
		vols(client)
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
		fmt.Fprintf(w, "id\tname\tup\tip\tports\tcmd\tage\timage")
	} else {
		fmt.Fprintf(w, "id\tname\tup\tip\tports\tage\timage")
	}
	for _, c := range containers {
		cinfo, err := client.InspectContainerWithOptions(
			docker.InspectContainerOptions{
				ID: c.ID,
			})
		if err != nil {
			log.Fatalf("InspectContainer: %s", err)
		}
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "%s", c.ID[:6])
		fmt.Fprintf(w, "\t%s", shorten(strings.TrimPrefix(cinfo.Name, "/"), int(0.2*width)))
		fmt.Fprintf(w, "\t%s", state(cinfo.State))

		// TODO, only one IP?
		ips := ips(c.Networks)
		fmt.Fprintf(w, "\t%s", ips[0])

		fmt.Fprintf(w, "\t%s", ports(c.Ports))

		if width > 100 {
			fmt.Fprintf(w, "\t%s", shorten(c.Command, int(0.15*width)))
		}

		fmt.Fprintf(w, "\t%s", prettyDuration(time.Since(time.Unix(c.Created, 0))))
		fmt.Fprintf(w, "\t%s", shorten(c.Image, int(0.2*width)))
	}
	fmt.Fprintf(w, "\n")
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
	fmt.Fprintf(w, "id\tage\tsize\trepotags")
	for _, i := range imgs {
		id := i.ID
		if strings.ContainsAny(i.ID, ":") {
			id = strings.SplitN(i.ID, ":", 2)[1]
		}
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "%s", id[:6])
		fmt.Fprintf(w, "\t%s", prettyDuration(time.Since(time.Unix(i.Created, 0))))
		fmt.Fprintf(w, "\t%s", shortenBytes(i.Size))
		fmt.Fprintf(w, "\t%s", strings.Join(i.RepoTags, " "))
	}
	fmt.Fprintf(w, "\n")
	w.Flush()
}

func vols(client *docker.Client) {
	vols, err := client.ListVolumes(docker.ListVolumesOptions{})
	if err != nil {
		log.Fatalf("ListVolumes: %s", err)
	}

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 2, 1, ' ', 0)
	fmt.Fprintf(w, "age\tdriver\tname")
	for _, v := range vols {
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "%s", prettyDuration(time.Since(v.CreatedAt)))
		fmt.Fprintf(w, "\t%s", v.Driver)
		fmt.Fprintf(w, "\t%s", v.Name)
	}
	fmt.Fprintf(w, "\n")
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
	var sb strings.Builder
	if !state.Running || state.Restarting {
		if state.Dead {
			return "dead"
		} else if state.StartedAt.IsZero() {
			return "created"
		} else if state.FinishedAt.IsZero() {
			return "FinishedAt==0"
		}
		if !state.Running {
			sb.WriteString("exit")
		} else {
			sb.WriteString("restart")
		}
		sb.WriteString(fmt.Sprintf("(%d)%s", state.ExitCode, prettyDuration(time.Since(state.FinishedAt))))
		return sb.String()
	}
	sb.WriteString(prettyDuration(time.Since(state.StartedAt)))
	if state.Paused {
		sb.WriteString("Paused")
	}
	return sb.String()
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

func ports(ports []docker.APIPort) string {
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
	return strings.Join(lines, ",")
}

func shorten(s string, l int) string {
	if len(s) > l {
		l--
		s = fmt.Sprintf("%s…%s", string([]rune(s)[:l/2+l%2]), string([]rune(s)[len(s)-l/2:]))
	}
	return strings.ReplaceAll(s, "\n", "␤")
}

func shortenBytes(bytes int64) string {
	byts := float64(bytes)
	unit := float64(1024)
	if byts < unit {
		return fmt.Sprintf("%d", bytes)
	}
	exp := math.Log(byts) / math.Log(unit)
	return fmt.Sprintf("%.1f%cB",
		byts/math.Pow(unit, math.Floor(exp)),
		"kMGTPE"[int(exp)-1])
}
