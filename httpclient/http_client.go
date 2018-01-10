package httpclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nienie/marathon/client"
	"github.com/nienie/marathon/config"
	"github.com/nienie/marathon/errors"
	"github.com/nienie/marathon/loadbalancer"
	"github.com/nienie/marathon/logger"

	transport "github.com/mreiferson/go-httpclient"
)

var (
	loggerHook AfterHTTHook = func(ctx context.Context, req *http.Request, resp *http.Response, err error) {
		format := "method=%s||host=%s||uri=%s||args=%s||body=%v||request_header=%v||response=%v||status_code=%d||response_header=%v||err=%v"
		if err != nil || resp == nil{
			logger.Warnf(ctx, format, req.Method, req.URL.Host, req.URL.Path, req.URL.RawQuery,
				req.Body, req.Header, nil, 0, nil, err)
			return
		}
		logger.Infof(ctx, format, req.Method, req.URL.Host, req.URL.Path, req.URL.RawQuery, req.Body,
			req.Header, resp.Body, resp.StatusCode, resp.Header, err)
		return
	}
)

//BeforeHTTPHook ...
type BeforeHTTPHook func(context.Context, *http.Request)

//AfterHTTHook ...
type AfterHTTHook func(context.Context, *http.Request, *http.Response, error)

//LoadBalancerHTTPClient ...
type LoadBalancerHTTPClient struct {
	*http.Client
	*loadbalancer.BaseLoadBalancerClient
	HTTPClientName string
	Transport      *transport.Transport
	BeforeHooks    []BeforeHTTPHook
	AfterHooks     []AfterHTTHook
}

//NewHTTPLoadBalancerClient ...
func NewHTTPLoadBalancerClient(clientConfig config.ClientConfig, lb loadbalancer.LoadBalancer) *LoadBalancerHTTPClient {
	//create load balancer context
	loadBalancerContext := loadbalancer.NewLoadBalancerContext(clientConfig, lb)
	//create load balancer client
	loadBalancerClient := &loadbalancer.BaseLoadBalancerClient{
		loadBalancerContext,
		nil,
	}
	//create transport
	trans := &transport.Transport{
		ConnectTimeout:   clientConfig.GetPropertyAsDuration(config.ConnectTimeout, config.DefaultConnectTimeout),
		ReadWriteTimeout: clientConfig.GetPropertyAsDuration(config.ReadWriteTimeout, config.DefaultReadWriteTimeout),
		RequestTimeout:   clientConfig.GetPropertyAsDuration(config.RequestTimeout, config.DefaultRequestTimeout),
	}
	//create original http.client
	originalClient := &http.Client{
		Transport: trans,
	}
	//create http client with load balancer
	httpClient := &LoadBalancerHTTPClient{
		Client:                 originalClient,
		BaseLoadBalancerClient: loadBalancerClient,
		HTTPClientName:         clientConfig.GetClientName(),
		Transport:              trans,
		BeforeHooks:            make([]BeforeHTTPHook, 0),
		AfterHooks:             []AfterHTTHook{loggerHook},
	}
	//load balancer context correlate with http client
	loadBalancerClient.Client = httpClient
	return httpClient
}

//Do ...
func (c *LoadBalancerHTTPClient) Do(ctx context.Context, request *HTTPRequest, requestConfig config.ClientConfig) (*http.Response, error) {
	if request == nil || request.Request == nil {
		return nil, fmt.Errorf("wrong type, request is nil")
	}
	req := request.GetRawRequest()
	c.beforeHTTPHook(ctx, req)
	resp, err := c.BaseLoadBalancerClient.ExecuteWithLoadBalancer(ctx, request, requestConfig)
	if err != nil || resp == nil {
		c.afterHTTPHook(ctx, req, nil, err)
		return nil, err
	}
	response := resp.(*HTTPResponse)
	c.afterHTTPHook(ctx, req, response.Response, err)
	return response.Response, nil
}

//Execute Do not Directly Use...
func (c *LoadBalancerHTTPClient) Execute(ctx context.Context, request client.Request, requestConfig config.ClientConfig) (client.Response, error) {
	req, ok := request.(*HTTPRequest)
	if !ok {
		return nil, errors.NewClientError(errors.General, fmt.Errorf("wrong type, type must be *HTTPRquest, type=%T", request))
	}
	return c.ExecuteHTTP(ctx, req, requestConfig)
}

//ExecuteHTTP Do not Directly Use...
func (c *LoadBalancerHTTPClient) ExecuteHTTP(ctx context.Context, request *HTTPRequest, requestConfig config.ClientConfig) (*HTTPResponse, error) {
	response, err := c.Client.Do(request.GetRawRequest())
	if err != nil {
		return nil, errors.ConvertError(err)
	}
	if response.StatusCode == http.StatusBadGateway ||
		response.StatusCode == http.StatusServiceUnavailable ||
		response.StatusCode == http.StatusGatewayTimeout { //502/503/504
		return nil, errors.NewClientError(errors.ServerThrottled, fmt.Errorf("http status code = %d", response.StatusCode))
	}
	return NewHTTPResponse(response), nil
}

//Shutdown ...
func (c *LoadBalancerHTTPClient) Shutdown() {
	c.Transport.Close()
}

//RegisterBeforeHook ...
func (c *LoadBalancerHTTPClient) RegisterBeforeHook(hooks ...BeforeHTTPHook) {
	c.BeforeHooks = append(c.BeforeHooks, hooks...)
}

//RegisterAfterHook ...
func (c *LoadBalancerHTTPClient) RegisterAfterHook(hooks ...AfterHTTHook) {
	c.AfterHooks = append(c.AfterHooks, hooks...)
}

func (c *LoadBalancerHTTPClient) beforeHTTPHook(ctx context.Context, req *http.Request) {
	for _, h := range c.BeforeHooks {
		h(ctx, req)
	}
}

func (c *LoadBalancerHTTPClient) afterHTTPHook(ctx context.Context, req *http.Request, resp *http.Response, err error) {
	for _, h := range c.AfterHooks {
		h(ctx, req, resp, err)
	}
}


