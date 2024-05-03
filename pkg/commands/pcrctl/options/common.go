package options

import (
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

// NewDefaultKubeletClientOptions 返回一个默认的 KubeletClientOptions
func NewDefaultKubeletClientOptions() KubeletClientOptions {
	return KubeletClientOptions{
		Server: "https://127.0.0.1:10250",
	}
}

// KubeletClientOptions Kubelet 客户端选项
type KubeletClientOptions struct {
	Server     string `json:"server,omitempty" yaml:"server,omitempty"`
	ServerName string `json:"serverName,omitempty" yaml:"serverName,omitempty"`
	Token      string `json:"token,omitempty" yaml:"token,omitempty"`
	CAFile     string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	CertFile   string `json:"certFile,omitempty" yaml:"certFile,omitempty"`
	KeyFile    string `json:"keyFile,omitempty" yaml:"keyFile,omitempty"`
}

// AddPFlags 将选项绑定到命令行参数
func (opts *KubeletClientOptions) AddPFlags(flags *pflag.FlagSet) {
	flags.StringVar(&opts.Server, "server", opts.Server, "The address of the Kubelet API server")
	flags.StringVar(
		&opts.ServerName, "server-name", opts.ServerName,
		"Server name to use for server certificate validation. "+
			"If it is not provided, the hostname used to contact the server is used",
	)
	flags.StringVar(&opts.Token, "token", opts.Token, "Bearer token for authentication to the API server")
	flags.StringVar(&opts.CAFile, "cacert", opts.CAFile, "Path to a cert file for the certificate authority")
	flags.StringVar(&opts.CertFile, "cert", opts.CertFile, "Path to a client certificate file for TLS")
	flags.StringVar(&opts.KeyFile, "key", opts.KeyFile, "Path to a client key file for TLS")
}

// ToClientConfig 基于选项获取 Kubelet 客户端配置
func (opts *KubeletClientOptions) ToClientConfig() *rest.Config {
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)

	insecure := false
	if opts.CAFile == "" {
		insecure = true
	}

	return &rest.Config{
		Host:        opts.Server,
		BearerToken: opts.Token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure:   insecure,
			ServerName: opts.ServerName,
			CAFile:     opts.CAFile,
			CertFile:   opts.CertFile,
			KeyFile:    opts.KeyFile,
		},
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{},
			NegotiatedSerializer: codecs.WithoutConversion(),
		},
		UserAgent: rest.DefaultKubernetesUserAgent(),
	}
}
