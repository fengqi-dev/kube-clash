package mcp

import (
	"context"
	"encoding/json"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/store"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyIn struct{}

type contextIn struct {
	Context string `json:"context" jsonschema:"Kubernetes context name"`
}

type contextNamespaceIn struct {
	Context   string `json:"context" jsonschema:"Kubernetes context name"`
	Namespace string `json:"namespace" jsonschema:"Kubernetes namespace"`
}

type idIn struct {
	ID string `json:"id" jsonschema:"Resource id returned by a previous start call"`
}

type manualNetworkIn struct {
	Context        string   `json:"context" jsonschema:"Kubernetes context name"`
	PodCIDRs       []string `json:"podCIDRs,omitempty" jsonschema:"Pod CIDR list"`
	ServiceCIDRs   []string `json:"serviceCIDRs,omitempty" jsonschema:"Service CIDR list"`
	DNSServer      string   `json:"dnsServer,omitempty" jsonschema:"CoreDNS / cluster DNS IP"`
	ClusterDomains []string `json:"clusterDomains,omitempty" jsonschema:"Cluster DNS domains; always includes cluster.local"`
	DNSNamespace   string   `json:"dnsNamespace,omitempty" jsonschema:"Namespace used for short-name DNS search"`
}

type hostAlias struct {
	Domain string `json:"domain" jsonschema:"DNS name"`
	IP     string `json:"ip" jsonschema:"IPv4 address"`
}

type hostAliasesIn struct {
	Context string      `json:"context" jsonschema:"Kubernetes context name"`
	Items   []hostAlias `json:"items" jsonschema:"Host aliases to set; empty clears"`
}

type portMappingIn struct {
	ServicePort int32  `json:"servicePort" jsonschema:"Service port number"`
	Protocol    string `json:"protocol,omitempty" jsonschema:"tcp or udp"`
	LocalHost   string `json:"localHost,omitempty" jsonschema:"Local bind host"`
	LocalPort   int    `json:"localPort" jsonschema:"Local listen port"`
}

type mappingIn struct {
	Namespace string          `json:"namespace" jsonschema:"Kubernetes namespace"`
	Service   string          `json:"service" jsonschema:"Service name"`
	Ports     []portMappingIn `json:"ports" jsonschema:"Port mappings"`
}

type previewIn struct {
	Namespace string          `json:"namespace" jsonschema:"Kubernetes namespace"`
	Name      string          `json:"name" jsonschema:"Preview Service name"`
	Ports     []portMappingIn `json:"ports" jsonschema:"Port mappings"`
}

type portForwardIn struct {
	Context    string `json:"context,omitempty" jsonschema:"Kubernetes context; defaults to active session"`
	Namespace  string `json:"namespace" jsonschema:"Kubernetes namespace"`
	Kind       string `json:"kind" jsonschema:"pod or service"`
	Name       string `json:"name" jsonschema:"Pod or Service name"`
	RemotePort uint16 `json:"remotePort" jsonschema:"Remote container/service port"`
	LocalPort  uint16 `json:"localPort,omitempty" jsonschema:"Local port; 0 allocates"`
}

func registerTools(server *mcpsdk.Server, backend Backend) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "reload_contexts",
		Description: "Reload kubeconfig inventory (contexts and kubeconfig files).",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, cluster.ClusterInventory, error) {
		out, err := backend.ReloadContexts()
		return nil, out, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "probe_context",
		Description: "Probe API Server reachability and version for a kubeconfig context.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextIn) (*mcpsdk.CallToolResult, cluster.ProbeResult, error) {
		out, err := backend.ProbeContext(ctx, in.Context)
		return nil, out, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_namespaces",
		Description: "List namespaces visible for a kubeconfig context.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextIn) (*mcpsdk.CallToolResult, namespacesOut, error) {
		out, err := backend.Namespaces(ctx, in.Context)
		return nil, namespacesOut{Namespaces: out}, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_services",
		Description: "List Services in a namespace.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextNamespaceIn) (*mcpsdk.CallToolResult, servicesOut, error) {
		out, err := backend.ListServices(ctx, in.Context, in.Namespace)
		return nil, servicesOut{Services: out}, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_pods",
		Description: "List Pods in a namespace.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextNamespaceIn) (*mcpsdk.CallToolResult, podsOut, error) {
		out, err := backend.ListPods(ctx, in.Context, in.Namespace)
		return nil, podsOut{Pods: out}, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "connect",
		Description: "Connect KubeLoop to a cluster context and namespace (starts tunnel).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextNamespaceIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.Connect(ctx, in.Context, in.Namespace); err != nil {
			return nil, nil, err
		}
		return nil, backend.SessionState(), nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "disconnect",
		Description: "Disconnect the active KubeLoop session.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.Disconnect(); err != nil {
			return nil, nil, err
		}
		return nil, backend.SessionState(), nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_manual_network",
		Description: "Get manual Pod/Service CIDR and DNS overrides for a context.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in contextIn) (*mcpsdk.CallToolResult, cluster.ManualNetwork, error) {
		return nil, backend.GetManualNetwork(in.Context), nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_manual_network",
		Description: "Set manual Pod/Service CIDR and DNS overrides for a context.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in manualNetworkIn) (*mcpsdk.CallToolResult, any, error) {
		err := backend.SetManualNetwork(in.Context, cluster.ManualNetwork{
			PodCIDRs:       in.PodCIDRs,
			ServiceCIDRs:   in.ServiceCIDRs,
			DNSServer:      in.DNSServer,
			ClusterDomains: in.ClusterDomains,
			DNSNamespace:   in.DNSNamespace,
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_host_aliases",
		Description: "Get local tunnel DNS host aliases for a context.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in contextIn) (*mcpsdk.CallToolResult, hostAliasesOut, error) {
		return nil, hostAliasesOut{Items: backend.GetHostAliases(in.Context)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_host_aliases",
		Description: "Replace local tunnel DNS host aliases for a context. Empty items clears.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in hostAliasesIn) (*mcpsdk.CallToolResult, any, error) {
		items := make([]store.HostAliasSpec, 0, len(in.Items))
		for _, item := range in.Items {
			items = append(items, store.HostAliasSpec{Domain: item.Domain, IP: item.IP})
		}
		if err := backend.SetHostAliases(in.Context, items); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "start_intercept",
		Description: "Start a Service Exchange (replace cluster Service traffic with a local process).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in mappingIn) (*mcpsdk.CallToolResult, intercept.Info, error) {
		out, err := backend.StartIntercept(ctx, toMapping(in))
		return nil, out, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "start_mirror",
		Description: "Start a Service Mirror (tee cluster Service traffic to a local process).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in mappingIn) (*mcpsdk.CallToolResult, intercept.Info, error) {
		out, err := backend.StartMirror(ctx, toMapping(in))
		return nil, out, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "stop_intercept",
		Description: "Stop an Exchange or Mirror by id.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in idIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.StopIntercept(ctx, in.ID); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_intercepts",
		Description: "List active Service Exchanges.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, interceptsOut, error) {
		return nil, interceptsOut{Items: backend.ListIntercepts()}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_mirrors",
		Description: "List active Service Mirrors.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, interceptsOut, error) {
		return nil, interceptsOut{Items: backend.ListMirrors()}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "start_preview",
		Description: "Create a temporary ClusterIP Service that points at a local process.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in previewIn) (*mcpsdk.CallToolResult, intercept.Info, error) {
		out, err := backend.StartPreview(ctx, intercept.PreviewRequest{
			Namespace: in.Namespace,
			Name:      in.Name,
			Ports:     toPortMappings(in.Ports),
		})
		return nil, out, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "stop_preview",
		Description: "Stop a Preview by id.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in idIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.StopPreview(ctx, in.ID); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_previews",
		Description: "List active Previews.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, interceptsOut, error) {
		return nil, interceptsOut{Items: backend.ListPreviews()}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "start_port_forward",
		Description: "Start an API Server port-forward to a Pod or Service.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in portForwardIn) (*mcpsdk.CallToolResult, portfwd.Info, error) {
		out, err := backend.StartPortForward(ctx, portfwd.Request{
			Context:    in.Context,
			Namespace:  in.Namespace,
			Kind:       in.Kind,
			Name:       in.Name,
			RemotePort: in.RemotePort,
			LocalPort:  in.LocalPort,
		})
		return nil, out, err
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "stop_port_forward",
		Description: "Stop a port-forward by id.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in idIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.StopPortForward(in.ID); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_port_forwards",
		Description: "List active port-forwards.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, portForwardsOut, error) {
		return nil, portForwardsOut{Items: backend.ListPortForwards()}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_singbox_config",
		Description: "Return the active session's sing-box config JSON. Requires an established connection.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, singboxConfigOut, error) {
		raw, err := backend.SingBoxConfig()
		if err != nil {
			return nil, singboxConfigOut{}, err
		}
		return nil, singboxConfigOut{Config: json.RawMessage(raw)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "helper_status",
		Description: "Return privileged virtual network helper install/run status.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, helperStatusOut, error) {
		st := backend.HelperStatus(ctx)
		return nil, helperStatusOut{
			Installed: st.Installed,
			Running:   st.Running,
			Version:   st.Version,
			Expected:  st.Expected,
			Socket:    st.Socket,
			Error:     st.Error,
		}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "install_helper",
		Description: "Install the privileged virtual network helper (may prompt for OS elevation).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.InstallHelper(ctx); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "uninstall_helper",
		Description: "Uninstall the privileged virtual network helper (may prompt for OS elevation).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, any, error) {
		if err := backend.UninstallHelper(ctx); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"ok": "true"}, nil
	})
}

type singboxConfigOut struct {
	Config json.RawMessage `json:"config"`
}

type helperStatusOut struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version,omitempty"`
	Expected  string `json:"expected"`
	Socket    string `json:"socket"`
	Error     string `json:"error,omitempty"`
}

type namespacesOut struct {
	Namespaces []string `json:"namespaces"`
}

type servicesOut struct {
	Services []cluster.ServiceInfo `json:"services"`
}

type podsOut struct {
	Pods []cluster.PodInfo `json:"pods"`
}

type hostAliasesOut struct {
	Items []store.HostAliasSpec `json:"items"`
}

type interceptsOut struct {
	Items []intercept.Info `json:"items"`
}

type portForwardsOut struct {
	Items []portfwd.Info `json:"items"`
}

func toMapping(in mappingIn) intercept.Mapping {
	return intercept.Mapping{
		Namespace: in.Namespace,
		Service:   in.Service,
		Ports:     toPortMappings(in.Ports),
	}
}

func toPortMappings(ports []portMappingIn) []intercept.PortMapping {
	out := make([]intercept.PortMapping, 0, len(ports))
	for _, port := range ports {
		out = append(out, intercept.PortMapping{
			ServicePort: port.ServicePort,
			Protocol:    port.Protocol,
			LocalHost:   port.LocalHost,
			LocalPort:   port.LocalPort,
		})
	}
	return out
}
