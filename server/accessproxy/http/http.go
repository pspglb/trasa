package http

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"

	"github.com/seknox/trasa/server/api/services"
	"github.com/seknox/trasa/server/global"
	"github.com/seknox/trasa/server/models"
	"github.com/seknox/trasa/server/utils"
	"github.com/sirupsen/logrus"
	"github.com/vulcand/oxy/forward"
)

var proxyConfig = make(map[string]models.ReverseProxy)
var trasaListenAddr = ""

// PrepareProxyConfig initializes available http proxy configs.
// This should ideally be self-reloading function based on config subscription (like how traefik reloads configs)
func PrepareProxyConfig() {
	trasaListenAddr = global.GetConfig().Trasa.ListenAddr
	allservices, err := services.Store.GetAllByType("http", global.GetConfig().Trasa.OrgId)
	if err != nil {
		// this is required. if we get error here, panic
		panic(err)
	}

	for _, v := range allservices {
		proxyConfig[v.Hostname] = v.ProxyConfig
	}

	return

}

// Proxy overtakes incoming http mux from caller and starts proxy service.
// Proxy forwarding is based on vulcand/oxy package
func Proxy() http.HandlerFunc {

	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Host == trasaListenAddr {
			logrus.Trace("reached 404 inside proxy")
			w.WriteHeader(404)
			return
		}

		err := tokenValidator(r, "", false)
		if err != nil {
			logrus.Debug(err)
			http.Redirect(w, r, fmt.Sprintf("https://%s/login#httphost=%s", trasaListenAddr, r.Host), 302)
			PrepareProxyConfig()
		}

		var upHost = utils.NormalizeString(proxyConfig[r.Host].UpstreamServer)
		if upHost == "" || len(upHost) < 4 {
			PrepareProxyConfig()
			upHost = utils.NormalizeString(proxyConfig[r.Host].UpstreamServer)
		}

		// let us forward this request to another server
		url, err := url.ParseRequestURI(upHost)
		if err != nil {
			logrus.Error(err)
			// TODO respond with error notification?
			return
		}
		r.URL = url

		insecureSkipVerify := false
		if proxyConfig[r.Host].StrictTLSValidation == false {
			insecureSkipVerify = true
		}

		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
		}

		// Forwards incoming requests to whatever location URL points to, adds proper forwarding headers
		fwd, err := forward.New(
			forward.PassHostHeader(proxyConfig[r.Host].PassHostheader),
			forward.RoundTripper(transport),
		)

		if err != nil {
			logrus.Error(err)
		}
		fwd.ServeHTTP(w, r)
	})

	return redirect

}
