package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"golang.org/x/term"
)

func main() {
	psCmd := flag.NewFlagSet("ps", flag.ExitOnError)
	psallFlag := psCmd.Bool("a", false, "show all containers")
	iCmd := flag.NewFlagSet("i", flag.ExitOnError)
	iallFlag := iCmd.Bool("a", false, "show all images")
	vCmd := flag.NewFlagSet("v", flag.ExitOnError)

	if len(os.Args) == 1 {
		fmt.Println("subcommands:")
		fmt.Println("  ps")
		fmt.Println("  i|imgs|images")
		fmt.Println("  v|vols|volumes")
		return
	}
	switch os.Args[1] {
	case "ps":
		psCmd.Parse(os.Args[2:])
		if psCmd.NArg() > 0 {
			fmt.Printf("Unexpected positional arguments.\n")
			os.Exit(2)
		}
		ps(*psallFlag)
	case "i", "imgs", "images":
		iCmd.Parse(os.Args[2:])
		if psCmd.NArg() > 0 {
			fmt.Printf("Unexpected positional arguments.\n")
			os.Exit(2)
		}
		imgs(*iallFlag)
	case "v", "vols", "volumes":
		vCmd.Parse(os.Args[2:])
		if psCmd.NArg() > 0 {
			fmt.Printf("Unexpected positional arguments.\n")
			os.Exit(2)
		}
		vols()
	default:
		fmt.Printf("%q: unknown subcommand.\n", os.Args[1])
		os.Exit(2)
	}
}

func newClient() *docker.Client {
	endpoint := "unix:///var/run/docker.sock"
	if dockerhost := os.Getenv("DOCKER_HOST"); dockerhost != "" {
		endpoint = dockerhost
	}

	client, err := docker.NewClient(endpoint)
	if err != nil {
		log.Fatalf("NewClient: %s", err)
	}
	return client
}

func ps(all bool) {
	client := newClient()
	containers, err := client.ListContainers(
		docker.ListContainersOptions{
			All: all, Size: false,
		})
	if err != nil {
		log.Fatalf("ListContainers: %s", err)
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Created < containers[j].Created
	})

	width := float64(termwidth())

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 2, 1, ' ', 0)
	if width > 100 {
		fmt.Fprintf(w, "id\tname\tage\tup\tip\tports\tcmd\timage (age)")
	} else {
		fmt.Fprintf(w, "id\tname\tage\tup\tip\tports\timage (age)")
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
		fmt.Fprintf(w, "\t%s", prettyDuration(time.Since(time.Unix(c.Created, 0))))
		fmt.Fprintf(w, "\t%s", state(cinfo.State))

		// TODO, only one IP?
		ips := ips(c.Networks)
		fmt.Fprintf(w, "\t%s", ips[0])

		fmt.Fprintf(w, "\t%s", ports(c.Ports))

		if width > 100 {
			fmt.Fprintf(w, "\t%s", shortenMiddle(c.Command, int(0.15*width)))
		}

		imgAge := "?"
		img, err := client.InspectImage(cinfo.Image) // by hash
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nInspectImage: %s\n", err)
		} else {
			imgAge = prettyDuration(time.Since(img.Created))
		}
		fmt.Fprintf(w, "\t%s (%s)", shorten(c.Image, int(0.2*width)), imgAge)
	}
	fmt.Fprintf(w, "\n")
	w.Flush()
}

func imgs(all bool) {
	client := newClient()
	imgs, err := client.ListImages(
		docker.ListImagesOptions{
			All: all,
		})
	if err != nil {
		log.Fatalf("ListImages: %s", err)
	}

	sort.Slice(imgs, func(i, j int) bool {
		return imgs[i].Created < imgs[j].Created
	})

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
		fmt.Fprintf(w, "\t%s", strings.Join(i.RepoTags, ","))
	}
	fmt.Fprintf(w, "\n")
	w.Flush()
}

func vols() {
	client := newClient()
	vols, err := client.ListVolumes(docker.ListVolumesOptions{})
	if err != nil {
		log.Fatalf("ListVolumes: %s", err)
	}

	sort.Slice(vols, func(i, j int) bool {
		return vols[i].CreatedAt.Before(vols[j].CreatedAt)
	})

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
		switch {
		case state.Dead:
			return "dead"
		case state.StartedAt.IsZero():
			return "created"
		case state.FinishedAt.IsZero():
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
	const (
		min   = 60
		hour  = 60 * min
		day   = 24 * hour
		week  = 7 * day
		month = 30 * day
		year  = 365 * day
	)
	s := int(duration.Seconds())
	switch {
	case s < 1:
		return "now"
	case s < min:
		return fmt.Sprintf("%ds", s)
	case s < hour:
		return fmt.Sprintf("%dm", s/min)
	case s < 3*day:
		return fmt.Sprintf("%dh", s/hour)
	case s < 2*week:
		return fmt.Sprintf("%dd", s/day)
	case s < 2*month:
		return fmt.Sprintf("%dw", s/week)
	case s < 2*year:
		return fmt.Sprintf("%dM", s/month)
	default:
		return fmt.Sprintf("%dy", s/year)
	}
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
		s = fmt.Sprintf("%s…", string([]rune(s)[:l]))
	}
	return strings.ReplaceAll(s, "\n", "␤")
}

func shortenMiddle(s string, l int) string {
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
