package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"golang.org/x/term"
)

// TODO
// - Allow specifying which obj types examine should look for

const (
	WIDE = 100
)

type allOpts struct {
	psAll     bool
	psVerbose bool
	iAll      bool
}

func main() {
	opts := allOpts{}
	psCmd := flag.NewFlagSet("ps", flag.ExitOnError)
	psCmd.BoolVar(&opts.psAll, "a", false, "show all containers")
	psCmd.BoolVar(&opts.psVerbose, "v", false, fmt.Sprintf(`verbose output, adds: age of container, ports listening IP,
cmd (always displayed if term width >= %d)`, WIDE))
	iCmd := flag.NewFlagSet("i", flag.ExitOnError)
	iCmd.BoolVar(&opts.iAll, "a", false, "show all images")
	vCmd := flag.NewFlagSet("v", flag.ExitOnError)
	xCmd := flag.NewFlagSet("x", flag.ExitOnError)

	if len(os.Args) == 1 {
		fmt.Println("subcommands:")
		fmt.Println("  ps|c|containers")
		fmt.Println("  i|imgs|images")
		fmt.Println("  v|vols|volumes")
		fmt.Println("  x|examine|inspect")
		return
	}
	switch os.Args[1] {
	case "ps", "c", "containers":
		err := psCmd.Parse(os.Args[2:])
		if err != nil {
			panic(err)
		}
		if psCmd.NArg() > 0 {
			fmt.Printf("Unexpected positional arguments.\n")
			os.Exit(2)
		}
		ps(opts)
	case "i", "imgs", "images":
		iCmd.Parse(os.Args[2:])
		if iCmd.NArg() > 0 {
			fmt.Printf("Unexpected positional arguments.\n")
			os.Exit(2)
		}
		imgs(opts)
	case "v", "vols", "volumes":
		vCmd.Parse(os.Args[2:])
		if vCmd.NArg() > 0 {
			fmt.Printf("Unexpected positional arguments.\n")
			os.Exit(2)
		}
		vols()
	case "x", "examine", "inspect":
		xCmd.Parse(os.Args[2:])
		if xCmd.NArg() != 1 {
			fmt.Printf("Expected 1 ID/name (prefix) to examine.\n")
			os.Exit(2)
		}
		examine(xCmd.Args()[0])
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

func ps(opts allOpts) {
	client := newClient()
	containers, err := client.ListContainers(
		docker.ListContainersOptions{
			All: opts.psAll, Size: false,
		})
	if err != nil {
		log.Fatalf("ListContainers: %s", err)
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Created < containers[j].Created
	})

	width := float64(termwidth())

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 1, ' ', 0)
	header := "id\tname"
	if opts.psVerbose {
		header += "\tage"
	}
	header += "\tup\tip\tports"
	if opts.psVerbose || width >= WIDE {
		header += "\tcmd"
	}
	header += "\timage\tage"
	fmt.Fprint(w, header)
	for _, c := range containers {
		cinfo, err := client.InspectContainerWithOptions(
			docker.InspectContainerOptions{ID: c.ID})
		if err != nil {
			log.Fatalf("InspectContainer: %s", err)
		}
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "%s", c.ID[:6])
		fmt.Fprintf(w, "\t%s", shorten(strings.TrimPrefix(cinfo.Name, "/"), int(0.2*width)))
		if opts.psVerbose {
			fmt.Fprintf(w, "\t%s", prettyDuration(time.Since(time.Unix(c.Created, 0))))
		}
		fmt.Fprintf(w, "\t%s", state(cinfo.State))

		// TODO, only one IP?
		ips := ips(c.Networks)
		fmt.Fprintf(w, "\t%s", ips[0])

		fmt.Fprintf(w, "\t%s", ports(c.Ports, opts.psVerbose))

		if opts.psVerbose || width >= WIDE {
			fmt.Fprintf(w, "\t%s", shortenMiddle(c.Command, int(0.15*width)))
		}

		imgAge := "?"
		img, err := client.InspectImage(cinfo.Image) // by hash
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nInspectImage: %s\n", err)
		} else {
			imgAge = prettyDuration(time.Since(img.Created))
		}
		fmt.Fprintf(w, "\t%s\t%s", shorten(c.Image, int(0.2*width)), imgAge)
	}
	fmt.Fprintf(w, "\n")
	w.Flush()
}

func imgs(opts allOpts) {
	client := newClient()
	imgs, err := client.ListImages(
		docker.ListImagesOptions{
			All: opts.iAll,
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

func examine(arg string) {
	client := newClient()
	container, err := client.InspectContainerWithOptions(
		docker.InspectContainerOptions{ID: arg})
	if err != nil {
		var errNoSuch *docker.NoSuchContainer
		if !errors.As(err, &errNoSuch) {
			log.Fatalf("InspectContainer: %s", err)
		}
	} else {
		outputFound(container, "container", container.ID)
		return
	}

	img, err := client.InspectImage(arg)
	if err != nil {
		if !errors.Is(err, docker.ErrNoSuchImage) {
			log.Fatalf("InspectImage: %s", err)
		}
	} else {
		outputFound(img, "image", img.ID)
		return
	}

	var vol *docker.Volume
	vols, err := client.ListVolumes(docker.ListVolumesOptions{})
	if err != nil {
		log.Fatalf("ListVolumes: %s", err)
	}
	for i := range vols {
		if strings.HasPrefix(vols[i].Name, arg) {
			if vol != nil {
				fmt.Fprintf(os.Stderr, "Found multiple volumes with prefix: %s\n", arg)
				return
			}
			vol = &vols[i]
		}
	}
	if vol != nil {
		outputFound(vol, "volume", vol.Name)
		return
	}

	fmt.Fprintf(os.Stderr, "Found nothing matching.\n")
}

func outputFound(obj interface{}, objType string, id string) {
	fmt.Fprintf(os.Stderr, "Found %s: %s\n", objType, id)
	b, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		log.Fatalf("Marshal: %s", err)
	}
	var out io.WriteCloser = os.Stdout
	if term.IsTerminal(int(os.Stdout.Fd())) {
		var cmd *exec.Cmd
		cmd, out = runPager()
		defer func() {
			out.Close()
			cmd.Wait()
		}()
	}
	fmt.Fprintf(out, "%s\n", b)
}

func runPager() (*exec.Cmd, io.WriteCloser) {
	pager := []string{"less"}
	if env := os.Getenv("PAGER"); env != "" {
		pager = strings.Split(os.Getenv("PAGER"), " ")
	}
	cmd := exec.Command(pager[0], pager[1:]...)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	return cmd, pipe
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
	case s < 2*day:
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

func ports(ports []docker.APIPort, verbose bool) string {
	lines := []string{}
	for _, p := range ports {
		pub := strconv.FormatInt(p.PublicPort, 10)
		priv := strconv.FormatInt(p.PrivatePort, 10)
		if p.Type != "tcp" {
			priv += "/" + p.Type
		}
		var line string
		if p.IP != "" {
			if verbose {
				line = net.JoinHostPort(p.IP, pub) + "→" + priv
			} else {
				line = pub + "→" + priv
			}
		} else {
			line = priv
		}
		if line != "" {
			if !contains(lines, line) {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, ",")
}

func contains(s []string, e string) bool {
	for i := range s {
		if s[i] == e {
			return true
		}
	}
	return false
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
