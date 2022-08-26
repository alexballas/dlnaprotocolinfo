package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/widget"
	"github.com/alexballas/go2tv/soapcalls"
	"github.com/gen2brain/dlgs"
	"github.com/koron/go-ssdp"
)

type root struct {
	XMLName xml.Name `xml:"root"`
	Device  device   `xml:"device"`
}

type device struct {
	XMLName     xml.Name    `xml:"device"`
	ServiceList serviceList `xml:"serviceList"`
}

type serviceList struct {
	XMLName  xml.Name  `xml:"serviceList"`
	Services []service `xml:"service"`
}

type service struct {
	XMLName     xml.Name `xml:"service"`
	Type        string   `xml:"serviceType"`
	ID          string   `xml:"serviceId"`
	ControlURL  string   `xml:"controlURL"`
	EventSubURL string   `xml:"eventSubURL"`
}

type dMRextracted struct {
	ConnectionManagerURL string
}

var (
	serverStarted = make(chan struct{})
	serverFailed  = make(chan error)
	Devices       = make(map[string]string)
)

func main() {

	myApp := app.New()
	myWindow := myApp.NewWindow("DLNA protocol info")
	but := widget.NewButton("Click me to get the DLNA protocol info for all supported devices in your network", func() {
		go func() {
			u, _ := url.Parse("http://localhost:13714/")
			_ = fyne.CurrentApp().OpenURL(u)
		}()
	})

	myApp.Lifecycle().SetOnStarted(func() {
		if err := loadSSDPservices(2); err != nil {
			dlgs.Error("Error", err.Error())
			os.Exit(1)
		}

		b, err := getResponseProtInfo()
		if err != nil {
			dlgs.Error("Error", err.Error())
			os.Exit(1)
		}

		c, err := getResponseConnectionIds()
		if err != nil {
			dlgs.Error("Error", err.Error())
			os.Exit(1)
		}

		go startServer(b + "\n~~~~~~~~~~~~~~~\n" + c)

		select {
		case <-serverStarted:
			break
		case e := <-serverFailed:
			dlgs.Error("Error", e.Error())
			os.Exit(1)
		}
	})
	myWindow.SetContent(but)
	myWindow.ShowAndRun()

}

func startServer(s string) error {
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc("/", serveData(s))

	ln, err := net.Listen("tcp", "localhost:13714")
	if err != nil {
		serverFailed <- err
		return fmt.Errorf("server listen error: %w", err)
	}

	serverStarted <- struct{}{}
	return server.Serve(ln)
}

func serveData(s string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(rw, "%s", s)
	}
}

func getResponseProtInfo() (string, error) {
	builder := new(strings.Builder)
	for q, w := range Devices {
		builder.WriteString("Protocol Info, Device: " + q + "\n")

		dmrStuff, err := dMRextractor(w)
		if err != nil {
			return "", err
		}

		client := &http.Client{}
		rawBody := `<?xml version='1.0' encoding='utf-8'?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:GetProtocolInfo xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"></u:GetProtocolInfo></s:Body></s:Envelope>`
		req, err := http.NewRequest(http.MethodPost, dmrStuff.ConnectionManagerURL, strings.NewReader(rawBody))
		if err != nil {
			return "", err
		}

		req.Header = http.Header{
			"SOAPAction":   []string{`"urn:schemas-upnp-org:service:ConnectionManager:1#GetProtocolInfo"`},
			"content-type": []string{"text/xml"},
			"charset":      []string{"utf-8"},
			"Connection":   []string{"close"},
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		bodybytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		builder.WriteString(string(bodybytes))
		builder.WriteString("\n----------\n")
	}
	return builder.String(), nil
}

func getResponseConnectionIds() (string, error) {
	builder := new(strings.Builder)
	for q, w := range Devices {
		builder.WriteString("ConnectionIds, Device: " + q + "\n")

		dmrStuff, err := dMRextractor(w)
		if err != nil {
			return "", err
		}

		client := &http.Client{}
		//rawBody := `<?xml version='1.0' encoding='utf-8'?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:GetProtocolInfo xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"></u:GetProtocolInfo></s:Body></s:Envelope>`
		rawBody := `<?xml version='1.0' encoding='utf-8'?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:GetCurrentConnectionIDs xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"></u:GetCurrentConnectionIDs></s:Body></s:Envelope>`
		req, err := http.NewRequest(http.MethodPost, dmrStuff.ConnectionManagerURL, strings.NewReader(rawBody))
		if err != nil {
			return "", err
		}

		req.Header = http.Header{
			"SOAPAction":   []string{`"urn:schemas-upnp-org:service:ConnectionManager:1#GetCurrentConnectionIDs"`},
			"content-type": []string{"text/xml"},
			"charset":      []string{"utf-8"},
			"Connection":   []string{"close"},
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		bodybytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		builder.WriteString(string(bodybytes))
		builder.WriteString("\n----------\n")
	}

	return builder.String(), nil
}

func loadSSDPservices(delay int) error {
	list, err := ssdp.Search(ssdp.All, delay, "")
	if err != nil {
		return fmt.Errorf("search error: %w", err)
	}

	for _, srv := range list {
		if srv.Type == "urn:schemas-upnp-org:service:AVTransport:1" {
			fn, err := soapcalls.GetFriendlyName(srv.Location)
			if err != nil {
				return err
			}
			Devices[fn] = srv.Location
		}
	}
	if len(Devices) > 0 {
		return nil
	}
	return errors.New("no available Media Renderers")
}

func dMRextractor(dmrurl string) (*dMRextracted, error) {
	var root root
	ex := &dMRextracted{}

	parsedURL, err := url.Parse(dmrurl)
	if err != nil {
		return nil, fmt.Errorf("dMRextractor parse error: %w", err)
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", dmrurl, nil)
	if err != nil {
		return nil, fmt.Errorf("dMRextractor GET error: %w", err)
	}

	xmlresp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dMRextractor Do GET error: %w", err)
	}
	defer xmlresp.Body.Close()

	xmlbody, err := io.ReadAll(xmlresp.Body)
	if err != nil {
		return nil, fmt.Errorf("dMRextractor read error: %w", err)
	}
	xml.Unmarshal(xmlbody, &root)
	for i := 0; i < len(root.Device.ServiceList.Services); i++ {
		if root.Device.ServiceList.Services[i].ID == "urn:upnp-org:serviceId:ConnectionManager" {
			ex.ConnectionManagerURL = parsedURL.Scheme + "://" + parsedURL.Host + root.Device.ServiceList.Services[i].ControlURL
		}
	}

	if ex.ConnectionManagerURL != "" {
		return ex, nil
	}

	return nil, errors.New("something broke somewhere - wrong DMR URL?")
}
