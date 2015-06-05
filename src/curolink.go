/*

    This project is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This project is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.
    You should have received a copy of the GNU Affero General Public License
    along with bostontraintrack.  If not, see <http://www.gnu.org/licenses/>.

*/


package main

import "fmt"
import "log"
import "net/http"

import "strings"
import "time"

import "regexp"

import "github.com/julienschmidt/httprouter"
import "github.com/abeconnelly/sloppyjson"

import "io/ioutil"

var CONFIG_FN string = "curolink.json"

var DEFAULT_REDIRECT string
var PORT string = ":80"

var g_verbose bool = false

type LocatorInfo struct {
  URL string
  Timestamp time.Time
}

var cache_server map[string]string
var cache_pdh map[string]LocatorInfo
var cache_uuid map[string]LocatorInfo

var g_cache_pdh_mutex chan bool
var g_cache_uuid_mutex chan bool


var target *string

var g_re_PDH *regexp.Regexp
var g_re_UUID *regexp.Regexp
var g_re_Project *regexp.Regexp
var g_re_Collection *regexp.Regexp

var g_help_bytes []byte
var g_favico_bytes []byte

func init() {

  g_verbose = true

  cache_server = make(map[string]string)
  cache_pdh = make(map[string]LocatorInfo)
  cache_uuid = make(map[string]LocatorInfo)

  g_cache_pdh_mutex = make(chan bool, 1)
  g_cache_uuid_mutex = make(chan bool, 1)

  g_cache_pdh_mutex <- true
  g_cache_uuid_mutex <- true

  var b []byte
  var e error
  var sj *sloppyjson.SloppyJSON


  b,e = ioutil.ReadFile(CONFIG_FN)
  if e!=nil { log.Fatal(e) }
  sj,e = sloppyjson.Loads(string(b))
  if e!=nil { log.Fatal(e) }

  for host,url := range sj.O["host"].O {
    cache_server[host] = fmt.Sprintf("https://%s", url.S)

    if g_verbose {
      log.Printf("host mapping: %s %s\n", host, cache_server[host])
    }
  }

  DEFAULT_REDIRECT = fmt.Sprintf("https://%s", sj.O["default_redirect"].S)

  if g_verbose {
    log.Printf("default_redirect: %s\n", DEFAULT_REDIRECT)
  }

  if _,ok := sj.O["port"] ; ok {
    PORT = fmt.Sprintf(":%s", sj.O["port"].S)
  }

  g_re_PDH,e = regexp.Compile(`^[0-9a-f]{32}\+[0-9]+$`)
  if e!=nil { log.Fatal(e) }
  g_re_UUID,e = regexp.Compile(`^[a-z0-9]{5}-[a-z0-9]{5}-[a-z0-9]{15}$`)
  if e!=nil { log.Fatal(e) }

  g_re_Project,e = regexp.Compile(`^[a-z0-9]{5}-j7d0g-[a-z0-9]{15}$`)
  if e!=nil { log.Fatal(e) }

  g_re_Collection,e = regexp.Compile(`^[a-z0-9]{5}-4zz18-[a-z0-9]{15}$`)
  if e!=nil { log.Fatal(e) }

  g_help_bytes,e = ioutil.ReadFile("html/help.html")
  if e!=nil { log.Fatal(e) }

  g_favico_bytes,e = ioutil.ReadFile("static/favicon.ico")
  if e!=nil { log.Fatal(e) }


  if g_verbose {
    log.Printf("init done\n")
  }


}

func write_cache_pdh(pdh string, info LocatorInfo) {
  <-g_cache_pdh_mutex
  cache_pdh[pdh] = info
  g_cache_pdh_mutex <- true
}

func write_cache_uuid(uuid string, info LocatorInfo) {
  <-g_cache_uuid_mutex
  cache_uuid[uuid] = info
  g_cache_uuid_mutex <- true
}

func about_redirect(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {
  if g_verbose {
    log.Printf("about (redirect)\n")
  }
  redirect_url := fmt.Sprintf("https://curoverse.com/about")
  http.Redirect(writer, req, redirect_url, 301)
  return
}

func curo_redirect(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {
  if g_verbose {
    log.Printf("curo (redirect)\n")
  }
  redirect_url := fmt.Sprintf("https://curoverse.com")
  http.Redirect(writer, req, redirect_url, 301)
  return
}

func help_page(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {
  if g_verbose { log.Printf("help\n") }
  writer.Write(g_help_bytes)
}

func favico(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {
  if g_verbose { log.Printf("favico\n") }
  writer.Write(g_favico_bytes)
}


func main() {

  fs_css := http.FileServer(http.Dir("static"))
  fs_js  := http.FileServer(http.Dir("static"))
  fs_img := http.FileServer(http.Dir("static"))

  rtr := httprouter.New()
  rtr.GET("/", curo_redirect)
  rtr.GET("/collections/*filepath", prox_collections)
  rtr.GET("/projects/*filepath", prox_projects)
  rtr.GET("/help", help_page)
  rtr.GET("/about", about_redirect)
  rtr.GET("/favicon.ico", favico)

  rtr.Handler("GET", "/images/*filepath", fs_img)
  rtr.Handler("GET", "/js/*filepath", fs_js)
  rtr.Handler("GET", "/css/*filepath", fs_css)

  rtr.NotFound = prox_simp

  //rtr.POST("/collections/*filepath", prox_collections)
  //rtr.POST("/projects/*filepath", prox_projects)

  //log.Fatal(http.ListenAndServe("127.0.0.1:8888", rtr))
  //log.Fatal(http.ListenAndServe(":8888", rtr))
  log.Fatal(http.ListenAndServe(PORT, rtr))
}

func prox_simp(writer http.ResponseWriter, req *http.Request) {
  prox(writer, req, nil)
}

func xfer_header(src http.Header, dst *http.Header) {
  for n,v := range src {
    for _,vv := range v {
      dst.Add(n, vv)
    }
  }
}


func prox_collections(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {
  if g_verbose { log.Printf("prox_collections\n") }
  prox(writer, req, param)
}

func prox_projects(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {
  if g_verbose { log.Printf("prox_projects\n") }
  prox(writer, req, param)
}

func update_object( obj_type, obj_id string ) {
  if c,ok := cache_uuid[obj_id] ; ok {
    c.Timestamp = time.Now()
    write_cache_uuid(obj_id, c)
    return
  }
  if c,ok := cache_pdh[obj_id] ; ok {
    c.Timestamp = time.Now()
    write_cache_pdh(obj_id, c)
    return
  }

  // Do it serially for now.  It would be nice
  // to do this asynchronisly to try and speed
  // up this process but it gets complicated.
  // I'm not sure how to do this asynchronisly
  // effectively.

  for name,fed_url := range cache_server {
    url := fmt.Sprintf("%s/%s/%s", fed_url, obj_type, obj_id)

    req, err := http.NewRequest("GET", url, nil)
    if err!=nil { log.Fatal(err) }

    var t http.Transport
    resp, err := t.RoundTrip(req)
    if err!=nil { log.Fatal(err) }
    defer resp.Body.Close()

    if g_verbose {
      log.Printf("cache query: %s %s %d\n", name, url, resp.StatusCode)
    }

    if resp.StatusCode == 200 {

      if g_verbose {
        log.Printf("CACHE ADD: %s -> %s\n", obj_id, url)
      }

      if obj_type == "collections" {
        loc := LocatorInfo{ url, time.Now() }
        write_cache_pdh(obj_id, loc)
      } else if obj_type == "projects" {
        loc := LocatorInfo{ url, time.Now() }
        write_cache_uuid(obj_id, loc)
      }

      return
    }

  }

}

func is_UUID(s string) bool {
  if g_re_UUID.MatchString(s) { return true }
  return false
}

func is_PDH(s string) bool {
  if g_re_PDH.MatchString(s) { return true }
  return false
}



func is_locator(s string) bool {
  if is_PDH(s) { return true }
  if is_UUID(s) { return true }
  return false
}

func is_uuid_collection(s string) bool {
  if s == "4zz18" { return true }
  return false;
}

func is_uuid_project(s string) bool {
  if s == "j7d0g" { return true }
  return false;
}

func prox(writer http.ResponseWriter, req *http.Request, param httprouter.Params) {


  // cut out beginning '/'
  //
  req_uri := string(req.RequestURI)
  orig_req_uri := req_uri
  if len(param.ByName("filepath"))>0 {
    req_uri = string(param.ByName("filepath"))
  }

  if g_verbose {
    log.Printf("prox: %s (orig:%s)\n", req_uri, orig_req_uri)
  }


  if len(req_uri)>0 { req_uri = req_uri[1:] }

  uri_parts := strings.Split(req_uri, "/")
  extra_path := ""
  if len(uri_parts)>1 {
    extra_path = fmt.Sprintf("/%s", strings.Join(uri_parts[1:], "/"))
  }

  redirect_url := fmt.Sprintf("%s/%s", DEFAULT_REDIRECT, req_uri)

  if is_PDH(uri_parts[0]) {

    if g_verbose {
      log.Printf("prox: PDH: %s\n", req_uri)
    }

    update_object("collections", uri_parts[0])
    update_object("projects", uri_parts[0])

    if loc,ok := cache_uuid[uri_parts[0]] ; ok {
      redirect_url = fmt.Sprintf("%s%s", loc.URL, extra_path)
    } else if loc,ok := cache_pdh[uri_parts[0]] ; ok {
      redirect_url = fmt.Sprintf("%s%s", loc.URL, extra_path)
    }

  } else if is_UUID(uri_parts[0]) {

    if g_verbose {
      log.Printf("prox: UUID: %s\n", req_uri)
    }

    uuid_parts := strings.Split(uri_parts[0], "-")
    host := uuid_parts[0]
    obj_type := uuid_parts[1]

    if is_uuid_project(obj_type) {
      if fed_host,ok := cache_server[host] ; ok {
        redirect_url = fmt.Sprintf("%s/projects/%s%s", fed_host, uri_parts[0], extra_path)
      }
    } else if is_uuid_collection(obj_type) {
      if fed_host,ok := cache_server[host] ; ok {
        redirect_url = fmt.Sprintf("%s/collections/%s%s", fed_host, uri_parts[0], extra_path)
      }
    }

  }

  if g_verbose {
    log.Printf("prox: final: %s -> %s\n", req_uri, redirect_url)
  }


  http.Redirect(writer, req, redirect_url, 301)
  return

}
