package send

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"git.sr.ht/~rjarry/aerc/lib/auth"
	"git.sr.ht/~rjarry/aerc/lib/proxy"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-smtp"
	"github.com/pkg/errors"
)

func connectSmtp(starttls bool, host string, domain string) (*smtp.Client, error) {
	serverName := host
	if !strings.ContainsRune(host, ':') {
		host += ":587"
	} else {
		serverName = host[:strings.IndexRune(host, ':')]
	}
	netConn, err := proxy.DialWithProxy("tcp", host, 30*time.Second)
	if err != nil {
		return nil, errors.Wrap(err, "smtp.Dial")
	}
	var conn *smtp.Client
	if starttls {
		conn, err = smtp.NewClientStartTLS(netConn, &tls.Config{ServerName: serverName})
		if err != nil {
			netConn.Close()
			return nil, errors.Wrap(err, "smtp.Dial")
		}
	} else {
		conn = smtp.NewClient(netConn)
	}
	if domain != "" {
		err := conn.Hello(domain)
		if err != nil {
			conn.Close()
			return nil, errors.Wrap(err, "Hello")
		}
	}
	return conn, nil
}

func connectSmtps(host string, domain string, insecure bool) (*smtp.Client, error) {
	serverName := host
	if !strings.ContainsRune(host, ':') {
		host += ":465"
	} else {
		serverName = host[:strings.IndexRune(host, ':')]
	}
	netConn, err := proxy.DialWithProxy("tcp", host, 30*time.Second)
	if err != nil {
		return nil, errors.Wrap(err, "smtp.DialTLS")
	}
	tlsConn := tls.Client(netConn, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: insecure,
	})
	conn := smtp.NewClient(tlsConn)
	if domain != "" {
		err := conn.Hello(domain)
		if err != nil {
			conn.Close()
			return nil, errors.Wrap(err, "Hello")
		}
	}
	return conn, nil
}

type smtpSender struct {
	conn *smtp.Client
	w    io.WriteCloser
}

func (s *smtpSender) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

func (s *smtpSender) Close() error {
	we := s.w.Close()
	ce := s.conn.Quit()
	if ce != nil {
		ce = s.conn.Close()
	}
	if we != nil {
		return we
	}
	return ce
}

func newSmtpSender(
	protocol string, mech string, uri *url.URL, domain string,
	from *mail.Address, rcpts []*mail.Address, account string,
	requestDSN bool,
) (io.WriteCloser, error) {
	var err error
	var conn *smtp.Client
	switch protocol {
	case "smtp":
		conn, err = connectSmtp(true, uri.Host, domain)
	case "smtp+insecure":
		conn, err = connectSmtp(false, uri.Host, domain)
	case "smtps":
		conn, err = connectSmtps(uri.Host, domain, false)
	case "smtps+insecure":
		conn, err = connectSmtps(uri.Host, domain, true)
	default:
		return nil, fmt.Errorf("not a smtp protocol %s", protocol)
	}

	if err != nil {
		return nil, errors.Wrap(err, "Connection failed")
	}

	if mech == "" && uri.User != nil {
		if conn.SupportsAuth("PLAIN") {
			mech = "plain"
		} else {
			mech = "login"
		}
	}

	saslclient, err := auth.NewSaslClient(mech, uri, account)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if saslclient != nil {
		if err := conn.Auth(saslclient); err != nil {
			conn.Close()
			return nil, errors.Wrap(err, "conn.Auth")
		}
	}
	s := &smtpSender{
		conn: conn,
	}
	if err := s.conn.Mail(from.Address, nil); err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "conn.Mail")
	}
	var rcptOptions *smtp.RcptOptions
	if requestDSN {
		rcptOptions = &smtp.RcptOptions{
			Notify: []smtp.DSNNotify{
				smtp.DSNNotifySuccess,
				smtp.DSNNotifyDelayed,
				smtp.DSNNotifyFailure,
			},
		}
	}
	for _, rcpt := range rcpts {
		if err := s.conn.Rcpt(rcpt.Address, rcptOptions); err != nil {
			conn.Close()
			return nil, errors.Wrap(err, "conn.Rcpt")
		}
	}
	s.w, err = s.conn.Data()
	if err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "conn.Data")
	}
	return s, nil
}
