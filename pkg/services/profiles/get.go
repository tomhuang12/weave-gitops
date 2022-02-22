package profiles

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/gogo/protobuf/jsonpb"
	pb "github.com/weaveworks/weave-gitops/pkg/api/profiles"
	"github.com/weaveworks/weave-gitops/pkg/helm/watcher/controller"
)

type GetOptions struct {
	Name      string
	Version   string
	Cluster   string
	Namespace string
	Writer    io.Writer
	Port      string
}

// Get returns a list of available profiles.
func (s *ProfilesSvc) Get(ctx context.Context, opts GetOptions) error {
	profiles, err := doKubeGetRequest(ctx, opts.Namespace, wegoServiceName, opts.Port, getProfilesPath, s.ClientSet)
	if err != nil {
		return err
	}

	printProfiles(profiles, opts.Writer)

	return nil
}

func doKubeGetRequest(ctx context.Context, namespace, serviceName, servicePort, path string, clientset kubernetes.Interface) (*pb.GetProfilesResponse, error) {
	resp, err := kubernetesDoRequest(ctx, namespace, wegoServiceName, "https", servicePort, getProfilesPath, clientset)
	if err != nil {
		return nil, err
	}

	profiles := &pb.GetProfilesResponse{}
	err = jsonpb.UnmarshalString(string(resp), profiles)

	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return profiles, nil
}

// GetProfile returns a single available profile.
func (s *ProfilesSvc) GetProfile(ctx context.Context, opts GetOptions) (*pb.Profile, string, error) {
	s.Logger.Actionf("getting available profiles in %s/%s", opts.Cluster, opts.Namespace)

	profilesList, err := doKubeGetRequest(ctx, opts.Namespace, wegoServiceName, opts.Port, getProfilesPath, s.ClientSet)
	if err != nil {
		return nil, "", err
	}

	var version string

	for _, p := range profilesList.Profiles {
		if p.Name == opts.Name {
			if len(p.AvailableVersions) == 0 {
				return nil, "", fmt.Errorf("no version found for profile '%s' in %s/%s", p.Name, opts.Cluster, opts.Namespace)
			}

			switch {
			case opts.Version == "latest":
				versions, err := controller.ConvertStringListToSemanticVersionList(p.AvailableVersions)
				if err != nil {
					return nil, "", err
				}

				controller.SortVersions(versions)
				version = versions[0].String()
			default:
				if !foundVersion(p.AvailableVersions, opts.Version) {
					return nil, "", fmt.Errorf("version '%s' not found for profile '%s' in %s/%s", opts.Version, opts.Name, opts.Cluster, opts.Namespace)
				}

				version = opts.Version
			}

			if p.GetHelmRepository().GetName() == "" || p.GetHelmRepository().GetNamespace() == "" {
				return nil, "", fmt.Errorf("HelmRepository's name or namespace is empty")
			}

			return p, version, nil
		}
	}

	return nil, "", fmt.Errorf("no available profile '%s' found in %s/%s", opts.Name, opts.Cluster, opts.Namespace)
}

func foundVersion(availableVersions []string, version string) bool {
	for _, v := range availableVersions {
		if v == version {
			return true
		}
	}

	return false
}

func printProfiles(profiles *pb.GetProfilesResponse, w io.Writer) {
	fmt.Fprintf(w, "NAME\tDESCRIPTION\tAVAILABLE_VERSIONS\n")

	if profiles.Profiles != nil && len(profiles.Profiles) > 0 {
		for _, p := range profiles.Profiles {
			fmt.Fprintf(w, "%s\t%s\t%v", p.Name, p.Description, strings.Join(p.AvailableVersions, ","))
			fmt.Fprintln(w, "")
		}
	}
}

func kubernetesDoRequest(ctx context.Context, namespace, serviceName, scheme, servicePort, path string, clientset kubernetes.Interface) ([]byte, error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	data, err := clientset.CoreV1().Services(namespace).ProxyGet(scheme, serviceName, servicePort, u.String(), nil).DoRaw(ctx)
	if err != nil {
		if se, ok := err.(*errors.StatusError); ok {
			return nil, fmt.Errorf("failed to make GET request to service %s/%s path %q status code: %d", namespace, serviceName, path, int(se.Status().Code))
		}

		return nil, fmt.Errorf("failed to make GET request to service %s/%s path %q: %w", namespace, serviceName, path, err)
	}

	return data, nil
}
