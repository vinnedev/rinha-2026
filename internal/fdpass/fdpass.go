// Package fdpass receives client TCP file descriptors over a Unix-domain
// SOCK_STREAM socket using SCM_RIGHTS ancillary data. The companion
// load balancer (e.g. jrblatt/so-no-forevis) accepts client connections
// on :9999 and sendmsg-passes the accepted fd to one of its upstream
// control sockets. The upstream consumes each fd as if it had accept()ed
// it locally, skipping a full proxy hop.
package fdpass
