/*
 * @Author: EagleXiang
 * @Github: https://github.com/eaglexiang
 * @Date: 2018-12-13 18:54:13
 * @LastEditors: EagleXiang
 * @LastEditTime: 2019-09-21 14:32:47
 */

package cmd

import (
	"errors"
	"net"
	"strings"
	"sync"

	"github.com/eaglexiang/eagle.tunnel.go/server/protocols/et/comm"
	cache "github.com/eaglexiang/go/cache/text"
	"github.com/eaglexiang/go/logger"
	"github.com/eaglexiang/go/tunnel"
)

// ErrADHostsFound 发现广告 hosts
var ErrADHostsFound = errors.New("ad hosts found")

// ErrInvalidProxyStatus 非法的 proxy-status
var ErrInvalidProxyStatus = errors.New("invalid proxy-status")

// DNS ET-DNS子协议的实现
type DNS struct {
	sync.Mutex
	DNSType        comm.CMDType
	dnsRemoteCache *cache.TextCache
	dnsLocalCache  *cache.TextCache
	DNSResolver    func(string) (string, error) `label:"DNS解析器"`
}

// Handle 处理ET-DNS请求
func (d *DNS) Handle(req string, t *tunnel.Tunnel) (err error) {
	reqs := strings.Split(req, " ")
	if len(reqs) < 2 {
		return errors.New("ETDNS.Handle -> req is too short")
	}

	e := comm.NetArg{NetConnArg: comm.NetConnArg{Domain: reqs[1]}}
	err = d.resolvDNSByLocal(&e)
	if err != nil {
		return
	}

	err = t.WriteLeftStr(e.IP)
	return
}

func (d *DNS) look4Hosts(domain string) (ip string, ok bool, err error) {
	ip, ok = comm.HostsCache[domain]
	if !ok {
		return
	}
	logger.Info("hosts found: ", domain, " ", ip)

	if ip == "::" {
		err = ErrADHostsFound
		logger.Info("ad hosts found: ", domain)
	}
	return
}

// Send 发送ET-DNS请求
func (d *DNS) Send(e *comm.NetArg) (err error) {
	logger.Info("resolv dns for domain: ", e.Domain)

	ip, ok, err := d.look4Hosts(e.Domain)
	if err != nil {
		return
	}
	if ok {
		e.IP = ip
		return
	}

	switch comm.DefaultArg.ProxyStatus {
	case comm.ProxySMART:
		err = d.smartSend(e)
	case comm.ProxyENABLE:
		err = d.proxySend(e)
	default:
		logger.Error("dns.send invalid proxy-status")
		err = ErrInvalidProxyStatus
	}
	logger.Info("ip for ", e.Domain, " is ", e.IP)
	return err
}

// smartSend 智能模式
// 智能模式会先检查域名是否存在于明确域名列表
// 列表内域名将根据明确规则进行解析
func (d *DNS) smartSend(e *comm.NetArg) (err error) {
	switch e.DomainType {
	case comm.DirectDomain:
		logger.Info("resolv direct domain: ", e.Domain)
		err = d.resolvDNSByLocal(e)
	case comm.ProxyDomain:
		logger.Info("resolv proxy domain: ", e.Domain)
		err = d.resolvDNSByProxy(e)
	default:
		logger.Info("resolv uncertain domain: ", e.Domain)
		err = d.resolvDNSByLocation(e)
	}
	return err
}

func (d *DNS) resolvDNSByLocation(e *comm.NetArg) (err error) {
	err = d.resolvDNSByLocal(e)
	// 判断IP所在位置是否适合代理
	comm.SubSenders[comm.LOCATION].Send(e)
	if !checkProxyByLocation(e.Location) {
		return nil
	}
	// 更新IP为Relay端的解析结果
	ne := &comm.NetArg{NetConnArg: comm.NetConnArg{Domain: e.Domain}}
	err = d.resolvDNSByProxy(ne)
	e.IP = ne.IP
	return
}

// proxySend 强制代理模式
func (d *DNS) proxySend(e *comm.NetArg) error {
	return d.resolvDNSByProxy(e)
}

// Type ET子协议类型
func (d *DNS) Type() comm.CMDType {
	return d.DNSType
}

// Name ET子协议的名字
func (d *DNS) Name() string {
	return comm.FormatEtType(d.DNSType)
}

func (d *DNS) getCacheNodeOfRemote(domain string) (node *cache.CacheNode, loaded bool) {
	if d.dnsRemoteCache == nil {
		d.Lock()
		if d.dnsRemoteCache == nil {
			logger.Info("create textcache for dns proxy")
			d.dnsRemoteCache = cache.CreateTextCache()
		}
		d.Unlock()
	}
	return d.dnsRemoteCache.Get(domain)
}

func (d *DNS) getCacheNodeOfLocal(domain string) (node *cache.CacheNode, loaded bool) {
	if d.dnsLocalCache == nil {
		d.Lock()
		if d.dnsLocalCache == nil {
			logger.Info("create textcache for dns local")
			d.dnsLocalCache = cache.CreateTextCache()
		}
		d.Unlock()
	}
	return d.dnsLocalCache.Get(domain)
}

// resolvDNSByProxy 使用代理服务器进行DNS的解析
// 此函数主要完成缓存功能
// 当缓存不命中则调用 DNS._resolvDNSByProxy
func (d *DNS) resolvDNSByProxy(e *comm.NetArg) (err error) {
	node, loaded := d.getCacheNodeOfRemote(e.Domain)
	if loaded {
		logger.Info("wait for proxy cachenode")
		e.IP, err = node.Wait()
	} else {
		err = d._resolvDNSByProxy(e)
		if err != nil {
			node.Destroy()
		} else {
			node.Update(e.IP)
		}
	}
	return err
}

// _resolvDNSByProxy 使用代理服务器进行DNS的解析
// 实际完成DNS查询操作
func (d *DNS) _resolvDNSByProxy(e *comm.NetArg) (err error) {
	e.IP, err = sendQuery(d, e.Domain)
	ip := net.ParseIP(e.IP)
	if ip == nil {
		logger.Warning("fail to resolv dns by proxy: ", e.Domain, " -> ", e.IP)
		return errors.New("invalid reply")
	}
	return nil
}

// resolvDNSByLocal 本地解析DNS
// 此函数主要完成缓存功能
// 当缓存不命中则进一步调用 DNS._resolvDNSByLocalClient
func (d *DNS) resolvDNSByLocal(e *comm.NetArg) (err error) {
	node, loaded := d.getCacheNodeOfLocal(e.Domain)
	if loaded {
		e.IP, err = node.Wait()
	} else {
		err = d._resolvDNSByLocal(e)
		if err != nil {
			node.Destroy()
		} else {
			node.Update(e.IP)
		}
	}
	return err
}

// _resolvDNSByLocalClient 本地解析DNS
// 实际完成DNS的解析动作
func (d *DNS) _resolvDNSByLocal(e *comm.NetArg) (err error) {
	e.IP, err = d.DNSResolver(e.Domain)
	// 本地解析失败应该让用户察觉，手动添加DNS白名单
	if err != nil {
		logger.Warning("fail to resolv dns by local, ",
			"consider adding this domain to your whitelist_domain.txt: ",
			e.Domain)
	}
	return err
}
