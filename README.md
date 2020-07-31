# web monitor

high performance web endpoint change monitoring. for comparing responses, a selected
list of http headers and the full response body is stored on a local key/value store file.

The following headers are included:

```go
var headerToInclude = []string{
	"Host",
	"Content-Length",
	"Content-Type",
	"Location",
	"Access-Control-Allow-Origin",
	"Access-Control-Allow-Methods",
	"Access-Control-Expose-Headers",
	"Access-Control-Allow-Credentials",
	"Allow",
	"Content-Security-Policy",
	"Proxy-Authenticate",
	"Server",
	"WWW-Authenticate",
	"X-Frame-Options",
	"X-Powered-By",
}
```

## usage

```
$ ./wonitor
NAME:
   wonitor - web monitor

USAGE:
   wonitor [global options] command [command options] [arguments...]

COMMANDS:
   add, a      add endpoint to monitor
   delete, d   deletes an endpoint
   get, g      get endpoint body
   list, l     list all monitored endpoints and their body size in bytes
   monitor, m  retrieve all urls and compare them
   help, h     Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h  show help (default: false)
λ wonitor (master) $ ./wonitor add --url https://unlink.io
λ wonitor (master) $ ./wonitor monitor --save
[https://unlink.io] 1576b diff:
HTTP/1.1 200 OK
Content-Type: text/html
Server: nginx/1.10.3 (Ubuntu)
X-Frame-Options: DENY

<html>
<body>
<pre>
[... snip ...]
</pre>
</body>
</html>

$ ./wonitor monitor --save
$ # no output because no change detected
```
