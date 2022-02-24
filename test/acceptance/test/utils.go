/**
* All common util functions and golbal constants will go here.
**/
package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"regexp"

	"strings"
	"time"

	"github.com/fluxcd/go-git-providers/github"
	"github.com/fluxcd/go-git-providers/gitlab"
	"github.com/fluxcd/go-git-providers/gitprovider"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/sclevine/agouti"
	log "github.com/sirupsen/logrus"
	wego "github.com/weaveworks/weave-gitops/api/v1alpha1"
	"github.com/weaveworks/weave-gitops/pkg/gitproviders"
	"github.com/weaveworks/weave-gitops/pkg/utils"
)

const (
	THIRTY_SECOND_TIMEOUT       time.Duration = 30 * time.Second
	EVENTUALLY_DEFAULT_TIMEOUT  time.Duration = 60 * time.Second
	INSTALL_RESET_TIMEOUT       time.Duration = 300 * time.Second
	NAMESPACE_TERMINATE_TIMEOUT time.Duration = 600 * time.Second
	INSTALL_SUCCESSFUL_TIMEOUT  time.Duration = 3 * time.Minute
	INSTALL_PODS_READY_TIMEOUT  time.Duration = 3 * time.Minute
	WEGO_DEFAULT_NAMESPACE                    = wego.DefaultNamespace
	WEGO_UI_URL                               = "http://localhost:9001"
	SELENIUM_SERVICE_URL                      = "http://localhost:4444/wd/hub"
	SCREENSHOTS_DIR             string        = "screenshots/"
	DEFAULT_BRANCH_NAME                       = "main"
	WEGO_DASHBOARD_TITLE        string        = "Weave GitOps"
	APP_PAGE_HEADER             string        = "Applications"
	charset                                   = "abcdefghijklmnopqrstuvwxyz0123456789"
	DEFAULT_K8S_VERSION         string        = "1.21.1"
)

var (
	githubOrg        string
	githubToken      string
	gitlabOrg        string
	gitlabToken      string
	gitopsBinaryPath string
)

type TestInputs struct {
	appRepoName         string
	appManifestFilePath string
	workloadName        string
	workloadNamespace   string
}

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}

	return true
}

func getClusterContext() string {
	out, err := exec.Command("kubectl", "config", "current-context").Output()
	Expect(err).ShouldNot(HaveOccurred())

	return string(bytes.TrimSuffix(out, []byte("\n")))
}

// showItems displays the current set of a specified object type in tabular format
func ShowItems(itemType string) error {
	if itemType != "" {
		return runCommandPassThrough([]string{}, "kubectl", "get", itemType, "--all-namespaces", "-o", "wide")
	}

	return runCommandPassThrough([]string{}, "kubectl", "get", "all", "--all-namespaces", "-o", "wide")
}

func ShowWegoControllerLogs(ns string) {
	controllers := []string{"helm", "kustomize", "notification", "source"}

	for _, c := range controllers {
		label := c + "-controller"
		log.Infof("Logs for controller: %s", label)
		_ = runCommandPassThrough([]string{}, "kubectl", "logs", "-l", "app="+label, "-n", ns)
	}
}

func StringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}

	return string(b)
}

func RandString(length int) string {
	return StringWithCharset(length, charset)
}

func generateTestInputs() TestInputs {
	var inputs TestInputs

	uniqueSuffix := RandString(6)
	inputs.appRepoName = "test-app-" + RandString(8)
	inputs.appManifestFilePath = getUniqueWorkload("xxyyzz", uniqueSuffix)
	inputs.workloadName = "nginx-" + uniqueSuffix
	inputs.workloadNamespace = "my-nginx-" + uniqueSuffix

	return inputs
}

func getUniqueWorkload(placeHolderSuffix string, uniqueSuffix string) string {
	workloadTemplateFilePath := "./data/nginx-template.yaml"
	absWorkloadManifestFilePath := "/tmp/nginx-" + uniqueSuffix + ".yaml"
	_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("sed 's/%s/%s/g' %s > %s", placeHolderSuffix, uniqueSuffix, workloadTemplateFilePath, absWorkloadManifestFilePath))

	return absWorkloadManifestFilePath
}

func ResetOrCreateCluster(namespace string, deleteWegoRuntime bool) (string, string, error) {
	return ResetOrCreateClusterWithName(namespace, deleteWegoRuntime, "", false)
}

func getK8sVersion() string {
	k8sVersion, found := os.LookupEnv("K8S_VERSION")
	if found {
		return k8sVersion
	}

	return DEFAULT_K8S_VERSION
}

func ResetOrCreateClusterWithName(namespace string, deleteWegoRuntime bool, clusterName string, keepExistingClusters bool) (string, string, error) {
	supportedProviders := []string{"kind", "kubectl"}
	supportedK8SVersions := []string{"1.19.1", "1.20.2", "1.21.1"}

	provider, found := os.LookupEnv("CLUSTER_PROVIDER")
	if keepExistingClusters || !found {
		provider = "kind"
	}

	k8sVersion := getK8sVersion()

	if !contains(supportedProviders, provider) {
		log.Errorf("Cluster provider %s is not supported for testing", provider)
		return clusterName, "", errors.New("Unsupported provider")
	}

	if !contains(supportedK8SVersions, k8sVersion) {
		log.Errorf("Kubernetes version %s is not supported for testing", k8sVersion)
		return clusterName, "", errors.New("Unsupported kubernetes version")
	}

	//For kubectl, point to a valid cluster, we will try to reset the namespace only
	if namespace != "" && provider == "kubectl" {
		err := runCommandPassThrough([]string{}, "./scripts/reset-wego.sh", namespace)
		if err != nil {
			log.Infof("Failed to reset the wego runtime in namespace %s", namespace)
		}

		if deleteWegoRuntime {
			uninstallWegoRuntime(namespace)
		}

		name, _ := runCommandAndReturnStringOutput("kubectl config current-context")
		clusterName = strings.TrimSuffix(name, "\n")
	}

	if provider == "kind" {
		var kindCluster string
		if clusterName == "" {
			kindCluster = RandString(6)
		}

		clusterName = provider + "-" + kindCluster

		log.Infof("Creating a kind cluster %s", kindCluster)

		var err error
		if keepExistingClusters {
			err = runCommandPassThrough([]string{}, "./scripts/kind-multi-cluster.sh", kindCluster, "kindest/node:v"+k8sVersion)
		} else {
			err = runCommandPassThrough([]string{}, "./scripts/kind-cluster.sh", kindCluster, "kindest/node:v"+k8sVersion)
		}

		if err != nil {
			log.Infof("Failed to create kind cluster")
			log.Fatal(err)

			return clusterName, "", err
		}
	}

	log.Info("Wait for the cluster to be ready")

	err := runCommandPassThrough([]string{}, "kubectl", "wait", "--for=condition=Ready", "--timeout=300s", "-n", "kube-system", "--all", "pods")

	if err != nil {
		log.Infof("Cluster system pods are not ready after waiting for 5 minutes, This can cause tests failures.")
		return clusterName, "", err
	}

	return clusterName, getClusterContext(), nil
}

func initAndCreateEmptyRepo(appRepoName string, providerName gitproviders.GitProviderName, isPrivateRepo bool, org string) string {
	repoAbsolutePath := "/tmp/" + appRepoName

	// We need this step in case running a single test case locally
	err := os.RemoveAll(repoAbsolutePath)
	Expect(err).ShouldNot(HaveOccurred())

	err = createGitRepository(appRepoName, DEFAULT_BRANCH_NAME, isPrivateRepo, providerName, org)
	Expect(err).ShouldNot(HaveOccurred())

	err = utils.WaitUntil(os.Stdout, time.Second*3, time.Second*30, func() error {
		command := exec.Command("sh", "-c", fmt.Sprintf(`
            git clone git@%s.com:%s/%s.git %s`,
			providerName, org, appRepoName,
			repoAbsolutePath))
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		err := command.Run()
		if err != nil {
			os.RemoveAll(repoAbsolutePath)
			return err
		}
		return nil
	})
	Expect(err).ShouldNot(HaveOccurred())

	return repoAbsolutePath
}

func waitForResource(resourceType string, resourceName string, namespace string, timeout time.Duration) error {
	pollInterval := 5

	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}

	timeoutInSeconds := int(timeout.Seconds())
	for i := pollInterval; i < timeoutInSeconds; i += pollInterval {
		log.Infof("Waiting for %s in namespace: %s... : %d second(s) passed of %d seconds timeout", resourceType+"/"+resourceName, namespace, i, timeoutInSeconds)
		err := runCommandPassThroughWithoutOutput([]string{}, "sh", "-c", fmt.Sprintf("kubectl get %s %s -n %s", resourceType, resourceName, namespace))

		if err == nil {
			log.Infof("%s is available in cluster", resourceType+"/"+resourceName)
			command := exec.Command("sh", "-c", fmt.Sprintf("kubectl get %s %s -n %s", resourceType, resourceName, namespace))
			session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(session).Should(gexec.Exit())

			noResourcesFoundMessage := fmt.Sprintf("No resources found in %s namespace", namespace)

			if strings.Contains(string(session.Wait().Out.Contents()), noResourcesFoundMessage) {
				log.Infof("Got message => {" + noResourcesFoundMessage + "} Continue looking for resource(s)")
				continue
			}

			return nil
		}

		time.Sleep(time.Duration(pollInterval) * time.Second)
	}

	return fmt.Errorf("Error: Failed to find the resource %s of type %s, timeout reached", resourceName, resourceType)
}

func waitForNamespaceToTerminate(namespace string, timeout time.Duration) error {
	//check if the namespace exist before cleaning up
	pollInterval := 5

	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}

	timeoutInSeconds := int(timeout.Seconds())

	err := runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl get ns %s", namespace))

	if err != nil {
		log.Infof("Namespace %s doesn't exist, nothing to clean — skipping...", namespace)
		return nil
	}

	for i := pollInterval; i < timeoutInSeconds; i += pollInterval {
		log.Infof("Waiting for namespace: %s to terminate : %d second(s) passed of %d seconds timeout", namespace, i, timeoutInSeconds)

		out, _ := runCommandAndReturnStringOutput(fmt.Sprintf("kubectl get ns %s --ignore-not-found=true | grep -i terminating", namespace))
		out = strings.TrimSpace(out)

		if out == "" {
			return nil
		}

		if i > timeoutInSeconds/2 && i%10 == 0 {
			//Patch the finalizer
			log.Infof("Patch the finalizer to unstuck the terminating namespace %s", namespace)
			_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl patch ns %s -p '{\"metadata\":{\"finalizers\":[]}}' --type=merge", namespace))
		}

		time.Sleep(time.Duration(pollInterval) * time.Second)
	}

	return fmt.Errorf("Error: Failed to terminate the namespace %s", namespace)
}

func VerifyControllersInCluster(namespace string) {
	Expect(waitForResource("deploy", "helm-controller", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("deploy", "kustomize-controller", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("deploy", "notification-controller", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("deploy", "source-controller", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("deploy", "image-automation-controller", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("deploy", "image-reflector-controller", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("deploy", "wego-app", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("pods", "", namespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())

	By("And I wait for the gitops controllers to be ready", func() {
		command := exec.Command("sh", "-c", fmt.Sprintf("kubectl wait --for=condition=Ready --timeout=%s -n %s --all pod --selector='app!=wego-app'", "120s", namespace))
		session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
		Expect(err).ShouldNot(HaveOccurred())
		Eventually(session, INSTALL_PODS_READY_TIMEOUT).Should(gexec.Exit())
	})
}

func installAndVerifyWego(wegoNamespace, repoURL string) {
	command := exec.Command("sh", "-c", fmt.Sprintf("%s install --namespace=%s --config-repo=%s --auto-merge", gitopsBinaryPath, wegoNamespace, repoURL))
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session, INSTALL_SUCCESSFUL_TIMEOUT).Should(gexec.Exit())
	Expect(string(session.Err.Contents())).Should(BeEmpty())
	VerifyControllersInCluster(wegoNamespace)
}

func uninstallWegoRuntime(namespace string) {
	log.Infof("About to delete Gitops runtime from namespace: %s", namespace)
	err := runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("%s flux uninstall --namespace %s --silent", gitopsBinaryPath, namespace))

	if err != nil {
		log.Infof("Failed to uninstall the gitops runtime %s", namespace)
	}

	err = runCommandPassThrough([]string{}, "sh", "-c", "kubectl delete crd apps.wego.weave.works")
	if err != nil {
		log.Infof("Failed to delete crd apps.wego.weave.works")
	}

	Expect(waitForNamespaceToTerminate(namespace, NAMESPACE_TERMINATE_TIMEOUT)).To(Succeed())
}

func deleteNamespace(namespace string) {
	log.Infof("Deleting namespace and all its resources: " + namespace)
	_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl delete all --all -n %s", namespace))

	command := exec.Command("kubectl", "delete", "ns", namespace)
	session, _ := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Eventually(session, NAMESPACE_TERMINATE_TIMEOUT).Should(gexec.Exit())
}

func deleteRepo(appRepoName string, providerName gitproviders.GitProviderName, org string) {
	log.Infof("Delete application repo: %s", org+"/"+appRepoName)

	gitProvider, orgRef, providerErr := getGitProvider(org, appRepoName, providerName)
	Expect(providerErr).ShouldNot(HaveOccurred())

	ctx := context.Background()
	or, repoErr := gitProvider.OrgRepositories().Get(ctx, orgRef)

	// allow repo to be absent (as tests assume this)
	if repoErr == nil {
		deleteErr := or.Delete(ctx)
		Expect(deleteErr).ShouldNot(HaveOccurred())
	}
}

func deleteWorkload(workloadName string, workloadNamespace string) {
	log.Infof("Delete the namespace %s along with workload %s", workloadNamespace, workloadName)
	_ = runCommandPassThrough([]string{}, "kubectl", "delete", "ns", workloadNamespace)
	_ = waitForNamespaceToTerminate(workloadNamespace, INSTALL_RESET_TIMEOUT)
}

func deletePersistingHelmApp(namespace string, workloadName string, timeout time.Duration) {
	//check if application exists before cleaning up
	err := runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl get -n %s pod/%s", namespace, workloadName))
	if err != nil {
		fmt.Println("No workloads exist under the namespace: " + namespace + ", nothing to clean — skipping...")
	} else {
		log.Infof("Found persisting helm workload under the namespace: " + namespace + ", cleaning up...")
		_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl delete -n %s helmreleases.helm.toolkit.fluxcd.io --all", namespace))
		_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl delete -n %s helmcharts.source.toolkit.fluxcd.io --all", namespace))
		_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl delete -n %s helmrepositories.source.toolkit.fluxcd.io --all", namespace))
		_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl delete apps -n %s --all", namespace))
		_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl wait --for=delete pod/%s -n %s --timeout=%s", workloadName, namespace, timeout))
	}
}

// Run a command, passing through stdout/stderr to the OS standard streams
func runCommandPassThrough(env []string, name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	if len(env) > 0 {
		cmd.Env = env
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func runCommandPassThroughWithoutOutput(env []string, name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	if len(env) > 0 {
		cmd.Env = env
	}

	return cmd.Run()
}

func runCommandAndReturnStringOutput(commandToRun string) (stdOut string, stdErr string) {
	command := exec.Command("sh", "-c", commandToRun)
	session, _ := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Eventually(session).Should(gexec.Exit())

	outContent := session.Wait().Out.Contents()
	errContent := session.Wait().Err.Contents()

	return string(outContent), string(errContent)
}

func runCommandAndReturnSessionOutput(commandToRun string) *gexec.Session {
	command := exec.Command("sh", "-c", commandToRun)
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())

	return session
}

func runWegoAddCommand(repoAbsolutePath string, addCommand string, wegoNamespace string) {
	log.Infof("Add command to run: %s in namespace %s from dir %s", addCommand, wegoNamespace, repoAbsolutePath)
	_, _ = runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, wegoNamespace)
}

func runWegoAddCommandWithOutput(repoAbsolutePath string, addCommand string, wegoNamespace string) (string, string) {
	command := exec.Command("sh", "-c", fmt.Sprintf("cd %s && %s %s", repoAbsolutePath, gitopsBinaryPath, addCommand))
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session).Should(gexec.Exit())

	return string(session.Wait().Out.Contents()), string(session.Wait().Err.Contents())
}

func verifyWegoAddCommand(appName string, wegoNamespace string) {
	command := exec.Command("sh", "-c", fmt.Sprintf(" kubectl wait --for=condition=Ready --timeout=60s -n %s GitRepositories --all", wegoNamespace))
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session, INSTALL_PODS_READY_TIMEOUT).Should(gexec.Exit())
	Expect(waitForResource("GitRepositories", appName, wegoNamespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
}

func verifyWegoHelmAddCommand(appName string, wegoNamespace string) {
	command := exec.Command("sh", "-c", fmt.Sprintf("kubectl wait --for=condition=Ready --timeout=60s -n %s HelmRepositories --all", wegoNamespace))
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session, INSTALL_PODS_READY_TIMEOUT).Should(gexec.Exit())
	Expect(waitForResource("HelmRepositories", appName, wegoNamespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
}

func verifyWorkloadIsDeployed(workloadName string, workloadNamespace string) {
	Expect(waitForResource("deploy", workloadName, workloadNamespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	Expect(waitForResource("pods", "", workloadNamespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	command := exec.Command("sh", "-c", fmt.Sprintf("kubectl wait --for=condition=Ready --timeout=60s -n %s --all pods --selector='app!=wego-app'", workloadNamespace))
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session, INSTALL_PODS_READY_TIMEOUT).Should(gexec.Exit())
}

func verifyHelmPodWorkloadIsDeployed(workloadName string, workloadNamespace string) {
	Expect(waitForResource("pods", workloadName, workloadNamespace, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
	c := fmt.Sprintf("kubectl wait --for=condition=Ready --timeout=360s -n %s --all pods --selector='app!=wego-app'", workloadNamespace)
	command := exec.Command("sh", "-c", c)
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session, INSTALL_PODS_READY_TIMEOUT).Should(gexec.Exit())
}

func gitAddCommitPush(repoAbsolutePath string, appManifestFilePath string) {
	command := exec.Command("sh", "-c", fmt.Sprintf(`
                            (cd %s && git pull origin main || true) &&
                            cp -r %s %s &&
                            cd %s &&
                            git add . &&
                            git commit -m 'add workload manifest' &&
                            git push -u origin main`,
		repoAbsolutePath, appManifestFilePath, repoAbsolutePath, repoAbsolutePath))
	session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(session, 10, 1).Should(gexec.Exit())
}

func setArtifactsDir() string {
	path := "/tmp/gitops-test"

	if os.Getenv("ARTIFACTS_BASE_DIR") == "" {
		return path
	}

	return os.Getenv("ARTIFACTS_BASE_DIR")
}

func initializeWebDriver(os string) {
	if os == "linux" {
		webDriver, err = agouti.NewPage(SELENIUM_SERVICE_URL, agouti.Desired(agouti.Capabilities{
			"chromeOptions": map[string][]string{
				"args": {
					"--disable-gpu",
					"--no-sandbox",
				}}}))
	}

	if os == "darwin" {
		chromeDriver := agouti.ChromeDriver(agouti.ChromeOptions("args", []string{"--disable-gpu", "--no-sandbox"}))
		err = chromeDriver.Start()
		Expect(err).NotTo(HaveOccurred())
		webDriver, err = chromeDriver.NewPage()
	}

	Expect(err).NotTo(HaveOccurred(), "Error creating new page")
}

func takeScreenshot() string {
	if webDriver != nil {
		t := time.Now()
		name := t.Format("Mon-02-Jan-2006-15.04.05.000000")
		filepath := path.Join(setArtifactsDir(), SCREENSHOTS_DIR, name+".png")
		_ = webDriver.Screenshot(filepath)

		logMsg := ("Screenshot function has been initiated; screenshot is saved in file: " + filepath + "\n")

		return logMsg
	}

	return ""
}

func getWaitTimeFromErr(errOutput string) (time.Duration, error) {
	var re = regexp.MustCompile(`(?m)\[rate reset in (.*)\]`)
	match := re.FindAllStringSubmatch(errOutput, -1)

	if len(match) >= 1 && len(match[1][0]) > 0 {
		duration, err := time.ParseDuration(match[1][0])
		if err != nil {
			return 0, fmt.Errorf("error pasing rate reset time %w", err)
		}

		return duration, nil
	}

	return 0, fmt.Errorf("could not found a rate reset on string: %s", errOutput)
}

func createGitRepository(repoName, branch string, private bool, providerName gitproviders.GitProviderName, org string) error {
	visibility := gitprovider.RepositoryVisibilityPublic
	if private {
		visibility = gitprovider.RepositoryVisibilityPrivate
	}

	description := "Weave Gitops test repo"
	defaultBranch := branch
	repoInfo := gitprovider.RepositoryInfo{
		Description:   &description,
		Visibility:    &visibility,
		DefaultBranch: &defaultBranch,
	}

	repoCreateOpts := &gitprovider.RepositoryCreateOptions{
		AutoInit: gitprovider.BoolVar(true),
	}

	gitProvider, orgRef, err := getGitProvider(org, repoName, providerName)
	if err != nil {
		return err
	}

	ctx := context.Background()

	fmt.Printf("creating repo %s ...\n", repoName)

	if err := utils.WaitUntil(os.Stdout, time.Second, THIRTY_SECOND_TIMEOUT, func() error {
		_, err := gitProvider.OrgRepositories().Create(ctx, orgRef, repoInfo, repoCreateOpts)
		if err != nil && strings.Contains(err.Error(), "rate limit exceeded") {
			waitForRateQuota, err := getWaitTimeFromErr(err.Error())
			if err != nil {
				return err
			}
			fmt.Printf("Waiting for rate quota %s \n", waitForRateQuota.String())
			time.Sleep(waitForRateQuota)
			return fmt.Errorf("retry after waiting for rate quota")
		}
		return err
	}); err != nil {
		return fmt.Errorf("error creating repo %s", err)
	}

	fmt.Printf("repo %s created ...\n", repoName)

	fmt.Printf("validating access to the repo %s ...\n", repoName)

	err = utils.WaitUntil(os.Stdout, time.Second, THIRTY_SECOND_TIMEOUT, func() error {
		_, err := gitProvider.OrgRepositories().Get(ctx, orgRef)
		return err
	})
	if err != nil {
		return fmt.Errorf("error validating access to the repository %w", err)
	}

	fmt.Printf("repo %s is accessible through the api ...\n", repoName)

	return nil
}

func getGitProvider(org string, repo string, providerName gitproviders.GitProviderName) (gitprovider.Client, gitprovider.OrgRepositoryRef, error) {
	var gitProvider gitprovider.Client

	var orgRef gitprovider.OrgRepositoryRef

	var err error

	switch providerName {
	case gitproviders.GitProviderGitHub:
		orgRef = gitproviders.NewOrgRepositoryRef(github.DefaultDomain, org, repo)

		gitProvider, err = github.NewClient(
			gitprovider.WithOAuth2Token(githubToken),
			gitprovider.WithDestructiveAPICalls(true),
		)
	case gitproviders.GitProviderGitLab:
		orgRef = gitproviders.NewOrgRepositoryRef(gitlab.DefaultDomain, org, repo)

		gitProvider, err = gitlab.NewClient(
			gitlabToken,
			"oauth2",
			gitprovider.WithOAuth2Token(gitlabToken),
			gitprovider.WithDestructiveAPICalls(true),
		)
	default:
		err = fmt.Errorf("invalid git provider name: %s", providerName)
	}

	return gitProvider, orgRef, err
}

func getGitProviderInfo() (gitproviders.GitProviderName, string, string) {
	gitlab := "gitlab"
	github := "github"
	gitProvider := os.Getenv("GIT_PROVIDER")

	if gitProvider == gitlab {
		log.Infof("Using git provider: %s", gitlab)
		return gitproviders.GitProviderGitLab, gitlabOrg, gitlab
	}

	log.Infof("Using git provider: %s", github)

	return gitproviders.GitProviderGitHub, githubOrg, github
}
