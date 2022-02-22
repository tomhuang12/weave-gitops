/**
* All tests related to 'gitops add' will go into this file
 */

package acceptance

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/weaveworks/weave-gitops/pkg/gitproviders"
	"github.com/weaveworks/weave-gitops/pkg/services/check"
)

var (
	clusterName      string
	clusterContext   string
	repoAbsolutePath string
)

var _ = Describe("Weave GitOps Add App Tests", func() {

	deleteWegoRuntime := false
	if os.Getenv("DELETE_WEGO_RUNTIME_ON_EACH_TEST") == "true" {
		deleteWegoRuntime = true
	}

	var _ = BeforeEach(func() {
		By("Given I have a brand new cluster", func() {
			var err error

			clusterName, clusterContext, err = ResetOrCreateCluster(WEGO_DEFAULT_NAMESPACE, deleteWegoRuntime)
			Expect(err).ShouldNot(HaveOccurred())
		})

		By("And I have a gitops binary installed on my local machine", func() {
			Expect(FileExists(gitopsBinaryPath)).To(BeTrue())
		})
	})

	It("Verify that gitops cannot work without gitops components installed OR with both url and directory provided", func() {
		var errOutput string
		var exitCode int
		private := true
		tip := generateTestInputs()
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"

		addCommand1 := "add app . --auto-merge=true"
		addCommand2 := "add app . --url=" + appRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And Gitops runtime is not installed", func() {
			uninstallWegoRuntime(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add command", func() {
			command := exec.Command("sh", "-c", fmt.Sprintf("cd %s && %s %s", repoAbsolutePath, gitopsBinaryPath, addCommand1))
			session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(session).Should(gexec.Exit())
			exitCode = session.Wait().ExitCode()
		})

		By("Then I should see relevant message in the console", func() {
			// Should  be a failure
			Eventually(exitCode).ShouldNot(Equal(0))
		})

		By("When I run add command with both directory path and url specified", func() {
			_, errOutput = runWegoAddCommandWithOutput(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see an error", func() {
			Expect(errOutput).To(ContainSubstring("you should choose either --url or the app directory"))
		})
	})

	It("Verify that gitops does not modify the cluster when run with --dry-run flag", func() {
		var addCommandOutput string
		var errOutput string
		private := true
		tip := generateTestInputs()
		branchName := "test-branch-01"
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"
		appName := tip.appRepoName

		addCommand := "add app --url=" + appRepoRemoteURL + " --branch=" + branchName + " --dry-run" + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I create a new branch", func() {
			createGitRepoBranch(repoAbsolutePath, branchName)
		})

		By("And I run 'gitops add app dry-run' command", func() {
			addCommandOutput, _ = runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see dry-run output with summary: name, url, path, branch and type", func() {
			Eventually(addCommandOutput).Should(MatchRegexp(`Name: ` + appName))
			Eventually(addCommandOutput).Should(MatchRegexp(`URL: ` + appRepoRemoteURL))
			Eventually(addCommandOutput).Should(MatchRegexp(`Path: ./`))
			Eventually(addCommandOutput).Should(MatchRegexp(`Branch: ` + branchName))
			Eventually(addCommandOutput).Should(MatchRegexp(`Type: kustomize`))
		})

		By("And I should not see any workload deployed to the cluster", func() {
			verifyWegoAddCommandWithDryRun(tip.appRepoName, WEGO_DEFAULT_NAMESPACE)
		})

		// Test for WGE
		By("When I try to upgrade gitops core to enterprise", func() {
			_, errOutput = runCommandAndReturnStringOutput(gitopsBinaryPath + " upgrade")
		})

		By("Then I should see error message", func() {
			Eventually(errOutput).Should(ContainSubstring("required flag(s) \"version\" not set"))
		})

		By("When I try to upgrade gitops core to enterprise and version provided", func() {
			_, errOutput = runCommandAndReturnStringOutput(gitopsBinaryPath + " upgrade --version=0.0.1")
		})

		By("Then I should see error message", func() {
			Eventually(errOutput).Should(ContainSubstring("failed to load credentials for profiles repo from cluster: failed to get entitlement: secrets \"weave-gitops-enterprise-credentials\" not found"))
		})
	})

	It("Test1 - Verify that gitops can deploy and delete an app after it is setup with an empty repo initially", func() {
		var appRemoveOutput string
		prLink := ""
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"

		addCommand := "add app . --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty private repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, private, gitOrg)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(gitOrg, tip.appRepoName, gitProvider)).Should(ContainSubstring("private"))
		})

		By("When I remove the app with auto-merge disabled", func() {
			appRemoveOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " delete app " + appName)

			re := regexp.MustCompile(`(http|ftp|https):\/\/([\w\-_]+(?:(?:\.[\w\-_]+)+))([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
			prLink = re.FindAllString(appRemoveOutput, -1)[0]
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(MatchRegexp(`► Removing application "` + appName + `" from cluster .* and repository`))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Pushing app changes to repository"))
		})

		By("And app should not get deleted until the PR is merged", func() {
			err = waitForAppRemoval(appName)
			Expect(err).To(HaveOccurred())
		})

		By("When I merge the PR for app deletion", func() {
			mergePR(repoAbsolutePath, prLink, gitProvider)
		})

		By("Then app should get deleted from the cluster", func() {
			err = waitForAppRemoval(appName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("Test1 - Verify that gitops can deploy and delete an app with separate config-repo over https protocol", func() {
		var appConfigRepoAbsPath string
		var appRemoveOutput string
		prLink := ""
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		appConfigRepoName := "config-repo-" + RandString(8)
		appRepoRemoteURL := "https://" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"
		configRepoRemoteURL := "https://" + gitProviderName + ".com/" + gitOrg + "/" + appConfigRepoName + ".git"

		addCommand := "add app --url=" + appRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
			deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath = initAndCreateEmptyRepo(appConfigRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(appConfigRepoAbsPath, tip.appManifestFilePath)
		})

		By("And I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoRemoteURL)
		})

		By("And I run gitops add command with --url", func() {
			runWegoAddCommand(repoAbsolutePath+"/../", addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("When I remove the app from the cluster", func() {
			appRemoveOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " delete app " + appName)

			re := regexp.MustCompile(`(http|ftp|https):\/\/([\w\-_]+(?:(?:\.[\w\-_]+)+))([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
			prLink = re.FindAllString(appRemoveOutput, -1)[0]
		})

		By("Then I should see PR created for app removal in config repo", func() {
			Eventually(appRemoveOutput).Should(MatchRegexp(`► Removing application "` + appName + `" from cluster .* and repository`))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Pushing app changes to repository"))
			Eventually(appRemoveOutput).Should(ContainSubstring("Pull Request created: https://" + gitProviderName + ".com/" + gitOrg + "/" + appConfigRepoName))
		})

		By("And app should remain in the cluster until PR is approved", func() {
			err = waitForAppRemoval(appName)
			Expect(err).To(HaveOccurred())
			appStatus, _ := runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName)
			Expect(appStatus).To(MatchRegexp(`kustomization/` + appName + `\s*True`))
		})

		By("When I merge PR for app deletion", func() {
			mergePR(appConfigRepoAbsPath, prLink, gitProvider)
		})

		By("Then app should get deleted from the cluster", func() {
			err = waitForAppRemoval(appName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("Test2 - Verify that gitops can deploy an app when provided with relative path: 'path/to/repo/dir'", func() {
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"

		addCommand := "add app " + tip.appRepoName + "/" + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run gitops add command from repo parent dir", func() {
			pathToRepoParentDir := repoAbsolutePath + "/../"
			runWegoAddCommand(pathToRepoParentDir, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(gitOrg, tip.appRepoName, gitProvider)).Should(ContainSubstring("private"))
		})
	})

	It("Test2 - Verify that gitops can deploy multiple workloads from a single app repo", func() {
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		appRepoName := "test-app-" + RandString(8)
		public := false
		appName := appRepoName
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appRepoName + ".git"

		addCommand := "add app . --name=" + appName + " --auto-merge=true"

		defer deleteRepo(appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repos do not already exist", func() {
			deleteRepo(appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("When I create an empty private repo for app1", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitProvider, public, gitOrg)
		})

		By("And I git add-commit-push for app with multiple workloads", func() {
			gitAddCommitPush(repoAbsolutePath, tip1.appManifestFilePath)
			gitAddCommitPush(repoAbsolutePath, tip2.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run gitops add command for all apps", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I should see multiple workloads deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})
	})

	It("Test2 - Verify that gitops can deploy multiple apps to the cluster using the same repo for config and sub-directories for apps", func() {
		private := true
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		appRepoName := "test-app-" + RandString(8)
		appName1 := "app1"
		appName2 := "app2"
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appRepoName + ".git"

		addCommand1 := "add app . --path=./" + appName1 + " --name=" + appName1 + " --auto-merge=true"
		addCommand2 := "add app . --path=./" + appName2 + " --name=" + appName2 + " --auto-merge=true"

		defer deleteRepo(appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("And I create a repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitProvider, private, gitOrg)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I add my app1 and app2 workloads and run the add app command for each app", func() {
			app1Path := createSubDir(appName1, repoAbsolutePath)
			app2Path := createSubDir(appName2, repoAbsolutePath)
			gitAddCommitPush(app1Path, tip1.appManifestFilePath)
			gitAddCommitPush(app2Path, tip2.appManifestFilePath)
			runWegoAddCommand(repoAbsolutePath, addCommand1, WEGO_DEFAULT_NAMESPACE)
			runWegoAddCommand(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workloads for app1 and app2 are deployed to the cluster", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})
	})

	It("Test3 - Verify that gitops can deploy 3 kinds of apps the cluster", func() {
		var configRepoRemoteURL string
		var listOutput string
		var appStatus1 string
		var appStatus2 string
		var appStatus3 string
		var commitList1 string
		var commitList2 string
		private := true
		readmeFilePath := "./data/README.md"
		tip := generateTestInputs()
		appFilesRepoName := tip.appRepoName
		appConfigRepoName := "config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appConfigRepoName + ".git"
		helmRepoURL := "https://charts.kube-ops.io"
		appName1 := appFilesRepoName
		workloadName1 := tip.workloadName
		workloadNamespace1 := tip.workloadNamespace
		appManifestFilePath1 := tip.appManifestFilePath
		appName2 := "my-helm-app"
		appManifestFilePath2 := "./data/helm-repo/hello-world"
		appName3 := "loki"
		workloadName3 := "loki-0"

		addCommand1 := "add app . --auto-merge=true"
		addCommand2 := "add app . --deployment-type=helm --path=./hello-world --name=" + appName2 + " --auto-merge=true"
		addCommand3 := "add app --url=" + helmRepoURL + " --chart=" + appName3 + " --auto-merge=true"

		defer deleteRepo(appFilesRepoName, gitProvider, gitOrg)
		defer deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		defer deleteWorkload(workloadName1, workloadNamespace1)
		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName3, EVENTUALLY_DEFAULT_TIMEOUT)

		By("And application repo does not already exist", func() {
			deleteRepo(appFilesRepoName, gitProvider, gitOrg)
			deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(workloadName1, workloadNamespace1)
			deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName3, EVENTUALLY_DEFAULT_TIMEOUT)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("When I create a private repo with app1 workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appFilesRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath1)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoRemoteURL)
		})

		By("And I run gitops add app command for app1: "+appName1, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand1, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app1", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(workloadName1, workloadNamespace1)
		})

		By("When I add manifests for app2", func() {
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath2)
		})

		By("And I run gitops add app command for app2: "+appName2, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app2", func() {
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			waitForResourceToExist("apps", appName2, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)
			waitForResourceToExist("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)
		})

		By("When I run gitops add app command for app3: "+appName3, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand3, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app3", func() {
			verifyWegoHelmAddCommand(appName3, WEGO_DEFAULT_NAMESPACE)
			verifyHelmPodWorkloadIsDeployed(workloadName3, WEGO_DEFAULT_NAMESPACE)
		})

		By("When I check the app status for app1", func() {
			appStatus1, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName1)
		})

		By("Then I should see the status for "+appName1, func() {
			Eventually(appStatus1).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus1).Should(ContainSubstring(`gitrepository/` + appName1))
			Eventually(appStatus1).Should(ContainSubstring(`kustomization/` + appName1))
		})

		By("When I check the app status for app2", func() {
			appStatus2, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName2)
		})

		By("Then I should see the status for "+appName2, func() {
			Eventually(appStatus2).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus2).Should(ContainSubstring(`gitrepository/` + appName2))
			Eventually(appStatus2).Should(ContainSubstring(`helmrelease/` + appName2))
		})

		By("When I check the app status for app3", func() {
			appStatus3, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName3)
		})

		By("Then I should see the status for "+appName3, func() {
			Eventually(appStatus3).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus3).Should(ContainSubstring(`helmrepository/` + appName3))
			Eventually(appStatus3).Should(ContainSubstring(`helmrelease/` + appName3))
		})

		By("When I check for apps", func() {
			listOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get apps")
		})

		By("Then I should see appNames for all apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName1))
			Eventually(listOutput).Should(ContainSubstring(appName2))
			Eventually(listOutput).Should(ContainSubstring(appName3))
		})

		By("And I should not see gitops components in app repo: "+appFilesRepoName, func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".weave-gitops"))
		})

		By("And I should see gitops components in config repo: "+appConfigRepoName, func() {
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && git clone %s && cd %s && ls -al", repoAbsolutePath, configRepoRemoteURL, appConfigRepoName))
			Expect(folderOutput).Should(ContainSubstring(".weave-gitops"))
		})

		By("When I check for list of commits for app1", func() {
			commitList1, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s get commits %s", gitopsBinaryPath, appName1))
		})

		By("Then I should see the list of commits for app1", func() {
			Eventually(commitList1).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
		})

		By("When I check for list of commits for app2", func() {
			commitList2, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s get commits %s", gitopsBinaryPath, appName2))
		})

		By("Then I should see the list of commits for app2", func() {
			Eventually(commitList2).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
		})
	})

	It("SmokeTestLong - Verify that gitops can deploy multiple apps one with private and other with public repo (e2e flow)", func() {
		var listOutput string
		var pauseOutput string
		var unpauseOutput string
		var appStatus1 *gexec.Session
		var appStatus2 *gexec.Session
		var appRemoveOutput string
		var repoAbsolutePath1 string
		var repoAbsolutePath2 string
		var appManifestFile1 string
		var commitList1 string
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		appName1 := tip1.appRepoName
		appName2 := tip2.appRepoName
		private := true
		public := false
		replicaSetValue := 3
		readmeFilePath := "./data/README.md"
		appConfigRepoName := "config-repo-" + RandString(8)
		configRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appConfigRepoName + ".git"

		addCommand1 := "add app . --name=" + appName1 + " --auto-merge=true"
		addCommand2 := "add app . --name=" + appName2 + " --auto-merge=true"

		defer deleteRepo(tip1.appRepoName, gitProvider, gitOrg)
		defer deleteRepo(tip2.appRepoName, gitProvider, gitOrg)
		defer deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repos do not already exist", func() {
			deleteRepo(tip1.appRepoName, gitProvider, gitOrg)
			deleteRepo(tip2.appRepoName, gitProvider, gitOrg)
			deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("And I create a private repo for app1", func() {
			repoAbsolutePath1 = initAndCreateEmptyRepo(tip1.appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath1, tip1.appManifestFilePath)
		})

		By("And I create a public repo for app2", func() {
			repoAbsolutePath2 = initAndCreateEmptyRepo(tip2.appRepoName, gitProvider, public, gitOrg)
			gitAddCommitPush(repoAbsolutePath2, tip2.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoRemoteURL)
		})

		By("And I run gitops add app command for 1st app", func() {
			runWegoAddCommand(repoAbsolutePath1, addCommand1, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add app command for 2nd app", func() {
			runWegoAddCommand(repoAbsolutePath2, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see workload for app1 is deployed to the cluster", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
		})

		By("And I should see workload for app2 is deployed to the cluster", func() {
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})

		By("And the repos created have proper visibility", func() {
			Eventually(getGitRepoVisibility(gitOrg, tip1.appRepoName, gitProvider)).Should(ContainSubstring("private"))
			if os.Getenv("GIT_PROVIDER") != "gitlab" {
				Eventually(getGitRepoVisibility(gitOrg, tip2.appRepoName, gitProvider)).Should(ContainSubstring("public"))
			}
		})

		By("When I check the app status for "+appName1, func() {
			appStatus1 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s get app %s", gitopsBinaryPath, appName1))
		})

		By("Then I should see the status for "+appName1, func() {
			Eventually(appStatus1).Should(gbytes.Say(`Last successful reconciliation:`))
			Eventually(appStatus1).Should(gbytes.Say(`gitrepository/` + appName1))
			Eventually(appStatus1).Should(gbytes.Say(`kustomization/` + appName1))
		})

		By("When I check the app status for "+appName2, func() {
			appStatus2 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s get app %s", gitopsBinaryPath, appName2))
		})

		By("Then I should see the status for "+appName2, func() {
			Eventually(appStatus2).Should(gbytes.Say(`Last successful reconciliation:`))
			Eventually(appStatus2).Should(gbytes.Say(`gitrepository/` + appName2))
			Eventually(appStatus2).Should(gbytes.Say(`kustomization/` + appName2))
		})

		By("When I check for apps", func() {
			listOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get apps")
		})

		By("Then I should see appNames for both apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName1))
			Eventually(listOutput).Should(ContainSubstring(appName2))
		})

		By("When I suspend an app: "+appName1, func() {
			pauseOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " suspend app " + appName1)
		})

		By("Then I should see pause message", func() {
			Expect(pauseOutput).To(ContainSubstring("gitops automation paused for " + appName1))
		})

		By("When I check app status for paused app", func() {
			appStatus1 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s get app %s", gitopsBinaryPath, appName1))
		})

		By("Then I should see pause status as suspended=true", func() {
			Eventually(appStatus1).Should(gbytes.Say(`kustomization/` + appName1 + `\s*True\s*.*True`))
		})

		By("And changes to the app files should not be synchronized", func() {
			appManifestFile1, _ = runCommandAndReturnStringOutput("cd " + repoAbsolutePath1 + " && ls | grep yaml")
			createAppReplicas(repoAbsolutePath1, appManifestFile1, replicaSetValue, tip1.workloadName)
			gitUpdateCommitPush(repoAbsolutePath1)
			err = waitForReplicaCreation(tip1.workloadNamespace, replicaSetValue, EVENTUALLY_DEFAULT_TIMEOUT)
			Expect(err).To(HaveOccurred())
		})

		By("And number of app replicas should remain same", func() {
			replicaOutput, _ := runCommandAndReturnStringOutput("kubectl get pods -n " + tip1.workloadNamespace + " --field-selector=status.phase=Running --no-headers=true | wc -l")
			Expect(replicaOutput).To(ContainSubstring("1"))
		})

		By("When I re-run suspend app command", func() {
			pauseOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " suspend app " + appName1)
		})

		By("Then I should see a console message without any errors", func() {
			Expect(pauseOutput).To(ContainSubstring("app " + appName1 + " is already paused"))
		})

		By("When I unpause an app: "+appName1, func() {
			unpauseOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " resume app " + appName1)
		})

		By("Then I should see unpause message", func() {
			Expect(unpauseOutput).To(ContainSubstring("gitops automation unpaused for " + appName1))
		})

		By("And I should see app replicas created in the cluster", func() {
			err = waitForReplicaCreation(tip1.workloadNamespace, replicaSetValue, EVENTUALLY_DEFAULT_TIMEOUT)
			Expect(err).NotTo(HaveOccurred())
			replicaOutput, _ := runCommandAndReturnStringOutput("kubectl get pods -n " + tip1.workloadNamespace + " --field-selector=status.phase=Running --no-headers=true | wc -l")
			Expect(replicaOutput).To(ContainSubstring(strconv.Itoa(replicaSetValue)))
		})

		By("When I re-run resume app command", func() {
			unpauseOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " resume app " + appName1)
		})

		By("Then I should see unpause message without any errors", func() {
			Expect(unpauseOutput).To(ContainSubstring("app " + appName1 + " is already reconciling"))
		})

		By("When I check app status for unpaused app", func() {
			appStatus1 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s get app %s", gitopsBinaryPath, appName1))
		})

		By("Then I should see pause status as suspended=false", func() {
			Eventually(appStatus1).Should(gbytes.Say(`kustomization/` + appName1 + `\s*True\s*.*False`))
		})

		By("When I delete an app with auto-merge enabled", func() {
			appRemoveOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " delete app " + appName2 + " --auto-merge")
		})

		By("Then I should see app deleting message", func() {
			Eventually(appRemoveOutput).Should(MatchRegexp(`► Removing application "` + appName2 + `" from cluster .* and repository`))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			err = waitForAppRemoval(appName2)
			Expect(err).NotTo(HaveOccurred())
		})

		By("When I check for list of commits for app1", func() {
			commitList1, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s get commits %s", gitopsBinaryPath, appName1))
		})

		By("Then I should see the list of commits for app1", func() {
			Eventually(commitList1).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]`))
		})
	})

	It("Verify that gitops can deploy a helm app from a git repo with config-repo set to default", func() {
		private := true
		appName := "my-helm-app"
		appManifestFilePath := "./data/helm-repo/hello-world"
		appRepoName := "test-app-" + RandString(8)
		appRepoRemoteURL := "https://" + gitProviderName + ".com/" + gitOrg + "/" + appRepoName + ".git"

		addCommand := "add app . --deployment-type=helm --path=./hello-world --name=" + appName + " --auto-merge=true"

		defer deleteRepo(appRepoName, gitProvider, gitOrg)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName, gitProvider, gitOrg)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			waitForResourceToExist("apps", appName, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)
			waitForResourceToExist("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)
		})

		By("And repo created has private visibility", func() {
			Eventually(getGitRepoVisibility(gitOrg, appRepoName, gitProvider)).Should(ContainSubstring("private"))
		})
	})

	It("Test3 - Verify that gitops can deploy a helm app from a git repo with config-repo set to <url>", func() {
		var configRepoAbsolutePath string
		private := true
		appManifestFilePath := "./data/helm-repo/hello-world"
		configRepoFiles := "./data/config-repo"
		appName := "my-helm-app"
		appRepoName := "test-app-" + RandString(8)
		configRepoName := "test-config-repo-" + RandString(8)
		configRepoUrl := fmt.Sprintf("ssh://git@"+gitProviderName+".com/%s/%s.git", gitOrg, configRepoName)

		addCommand := fmt.Sprintf("add app . --deployment-type=helm --path=./hello-world --name=%s --auto-merge=true", appName)

		defer deleteRepo(appRepoName, gitProvider, gitOrg)
		defer deleteRepo(configRepoName, gitProvider, gitOrg)

		By("Application and config repo does not already exist", func() {
			deleteRepo(appRepoName, gitProvider, gitOrg)
			deleteRepo(configRepoName, gitProvider, gitOrg)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("When I create a private repo for my config files", func() {
			configRepoAbsolutePath = initAndCreateEmptyRepo(configRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(configRepoAbsolutePath, configRepoFiles)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoUrl)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("And there is no .weave-gitops folder in the app repo", func() {
			_, err := os.Stat(repoAbsolutePath + "/.weave-gitops")
			Expect(os.IsNotExist(err)).To(Equal(true))
		})

		By("And the manifests are present in the config repo", func() {
			out, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && git pull origin main", configRepoAbsolutePath))
			Eventually(out).Should(ContainSubstring(`apps/` + appName + `/app.yaml`))
			Eventually(out).Should(MatchRegexp(`apps/` + appName + `/kustomization.yaml`))
			Eventually(out).Should(MatchRegexp(`apps/` + appName + `/` + appName + `-gitops-source.yaml`))
			Eventually(out).Should(MatchRegexp(`apps/` + appName + `/` + appName + `-gitops-deploy.yaml`))
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			waitForResourceToExist("apps", appName, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)
			waitForResourceToExist("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)
		})

	})

	It("Test3 - Verify that gitops can deploy multiple helm apps from a helm repo with config-repo set to <url>", func() {
		var listOutput string
		var appStatus1 string
		var appStatus2 string
		var workloadName2 string
		var workloadName3 string
		private := true
		appName1 := "loki"
		appName2 := "promtail"
		workloadNamespace := "test-space"
		workloadName1 := workloadNamespace + "-loki-0"
		readmeFilePath := "./data/README.md"
		appRepoName := "test-app-" + RandString(8)
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appRepoName + ".git"
		helmRepoURL := "https://charts.kube-ops.io"

		addCommand1 := "add app --url=" + helmRepoURL + " --chart=" + appName1 + " --auto-merge=true --helm-release-target-namespace=" + workloadNamespace
		addCommand2 := "add app --url=" + helmRepoURL + " --chart=" + appName2 + " --auto-merge=true --helm-release-target-namespace=" + workloadNamespace

		defer deleteNamespace(workloadNamespace)
		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName1, EVENTUALLY_DEFAULT_TIMEOUT)
		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName2, EVENTUALLY_DEFAULT_TIMEOUT)
		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName3, EVENTUALLY_DEFAULT_TIMEOUT)
		defer deleteRepo(appRepoName, gitProvider, gitOrg)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName1, EVENTUALLY_DEFAULT_TIMEOUT)
		})

		By("When I create a private git repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, readmeFilePath)
		})

		By("And I install gitops under my namespace: "+WEGO_DEFAULT_NAMESPACE, func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I create a namespace for helm-app", func() {
			out, _ := runCommandAndReturnStringOutput("kubectl create ns " + workloadNamespace)
			Eventually(out).Should(ContainSubstring("namespace/" + workloadNamespace + " created"))
		})

		By("And I run gitops add app command for 1st app", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand1, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add app command for 2nd app", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see workload1 deployed to the cluster", func() {
			verifyWegoHelmAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyHelmPodWorkloadIsDeployed(workloadName1, workloadNamespace)
		})

		By("And I should see workload2 deployed to the cluster", func() {
			verifyWegoHelmAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)

			out, _ := runCommandAndReturnStringOutput("kubectl get pods -A --no-headers -o custom-columns=':metadata.name' | grep " + appName2)
			temp := strings.Split(out, "\n")

			workloadName2 = strings.TrimSpace(temp[0])
			workloadName3 = strings.TrimSpace(temp[1])

			verifyHelmPodWorkloadIsDeployed(workloadName2, workloadNamespace)
			verifyHelmPodWorkloadIsDeployed(workloadName3, workloadNamespace)
		})

		By("And I should see gitops components in the remote git repo", func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).Should(ContainSubstring(".weave-gitops"))
		})

		By("When I check for apps", func() {
			listOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get apps")
		})

		By("Then I should see appNames for both apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName1))
			Eventually(listOutput).Should(ContainSubstring(appName2))
		})

		By("When I check the app status for "+appName1, func() {
			appStatus1, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName1)
		})

		By("Then I should see the status for app1", func() {
			Eventually(appStatus1).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus1).Should(ContainSubstring(`helmrepository/` + appName1))
			Eventually(appStatus1).Should(ContainSubstring(`helmrelease/` + appName1))
		})

		By("When I check the app status for "+appName2, func() {
			appStatus2, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName2)
		})

		By("Then I should see the status for app2", func() {
			Eventually(appStatus2).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus2).Should(ContainSubstring(`helmrepository/` + appName2))
			Eventually(appStatus2).Should(ContainSubstring(`helmrelease/` + appName2))
		})
	})

	It("@gitlabTests Test2 - Verify that gitops can deploy and delete a public gitlab app with separate config-repo", func() {
		var appRemoveOutput string
		public := false
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		configRepoName := "config-" + appName

		configRepoURL := "ssh://git@gitlab.com/" + gitlabPublicGroup + "/" + configRepoName + ".git"

		addCommand := "add app . --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitproviders.GitProviderGitLab, gitlabPublicGroup)
		defer deleteRepo(configRepoName, gitproviders.GitProviderGitLab, gitlabPublicGroup)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("I have my default ssh key on path "+sshKeyPath, func() {
			setupGitlabSSHKey(sshKeyPath)
		})

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitproviders.GitProviderGitLab, gitlabPublicGroup)
			deleteRepo(configRepoName, gitproviders.GitProviderGitLab, gitlabPublicGroup)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty public repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitLab, public, gitlabPublicGroup)
			_ = initAndCreateEmptyRepo(configRepoName, gitproviders.GitProviderGitLab, private, gitlabPublicGroup)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoURL)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have public visibility", func() {
			Expect(getGitRepoVisibility(gitlabPublicGroup, tip.appRepoName, gitproviders.GitProviderGitLab)).Should(ContainSubstring("public"))
		})

		By("When I remove the app with auto-merge enabled", func() {
			appRemoveOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " delete app " + appName + " --auto-merge")
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(MatchRegexp(`► Removing application "` + appName + `" from cluster .* and repository`))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			err = waitForAppRemoval(appName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("@gitlabTests Test2 - Verify that gitops can deploy a public gitlab app", func() {
		public := false
		tip := generateTestInputs()
		appName := tip.appRepoName
		appRepoRemoteURL := "ssh://git@gitlab.com/" + gitlabPublicGroup + "/" + tip.appRepoName + ".git"

		addCommand := "add app . --auto-merge=true"

		defer deleteRepo(tip.appRepoName, gitproviders.GitProviderGitLab, gitlabPublicGroup)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("I have my default ssh key on path "+sshKeyPath, func() {
			setupGitlabSSHKey(sshKeyPath)
		})

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitproviders.GitProviderGitLab, gitlabPublicGroup)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty public repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitLab, public, gitlabPublicGroup)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have public visibility", func() {
			Expect(getGitRepoVisibility(gitlabPublicGroup, tip.appRepoName, gitproviders.GitProviderGitLab)).Should(ContainSubstring("public"))
		})
	})

	It("@gitlabTests Test3 - Verify that gitops can deploy and delete a gitlab app in a subgroup", func() {
		var appRemoveOutput string
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName

		addCommand := "add app . --auto-merge=true"

		subGroup := gitlabOrg + "/" + gitlabSubgroup

		appRepoRemoteURL := "ssh://git@gitlab.com/" + subGroup + "/" + appName + ".git"

		defer deleteRepo(tip.appRepoName, gitproviders.GitProviderGitLab, subGroup)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("I have my default ssh key on path "+sshKeyPath, func() {
			setupGitlabSSHKey(sshKeyPath)
		})

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitproviders.GitProviderGitLab, subGroup)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty private repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitLab, private, subGroup)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(subGroup, tip.appRepoName, gitproviders.GitProviderGitLab)).Should(ContainSubstring("private"))
		})

		By("When I remove an app with auto-merge disabled", func() {
			appRemoveOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " delete app " + appName + " --auto-merge")
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(MatchRegexp(`► Removing application "` + appName + `" from cluster .* and repository`))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			err = waitForAppRemoval(appName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("Test2 - Verify that a PR is raised against a user repo when skipping auto-merge", func() {
		tip := generateTestInputs()
		appName := tip.appRepoName
		prLink := ""
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"

		addCommand := "add app . --name=" + appName + " --auto-merge=false"

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		})

		By("When I create an empty private repo for app", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, true, gitOrg)
		})

		By("And I git add-commit-push app manifest", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("When I run gitops add app command for app", func() {
			output, _ := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
			re := regexp.MustCompile(`(http|ftp|https):\/\/([\w\-_]+(?:(?:\.[\w\-_]+)+))([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
			prLink = re.FindAllString(output, -1)[0]
		})

		By("Then I should see a PR created in user repo", func() {
			verifyPRCreated(repoAbsolutePath, appName, gitProvider)
		})

		By("When I merge the created PR", func() {
			mergePR(repoAbsolutePath, prLink, gitProvider)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

	It("Test2 - Verify that a PR can be raised against an external repo with config-repo set to <url>", func() {
		var configRepoRemoteURL string
		var appConfigRepoAbsPath string
		prLink := ""
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		appConfigRepoName := "config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appConfigRepoName + ".git"

		addCommand := "add app ."

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
			deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath = initAndCreateEmptyRepo(appConfigRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(appConfigRepoAbsPath, tip.appManifestFilePath)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoRemoteURL)
		})

		By("And I run gitops add command", func() {
			output, _ := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
			re := regexp.MustCompile(`(http|https):\/\/([\w\-_]+(?:(?:\.[\w\-_]+)+))([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
			prLink = re.FindAllString(output, 1)[0]
		})

		By("Then I should see a PR created for external repo", func() {
			verifyPRCreated(appConfigRepoAbsPath, appName, gitProvider)
		})

		By("When I merge the created PR", func() {
			mergePR(appConfigRepoAbsPath, prLink, gitProvider)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

	It("Verify that a PR fails when raised against the same app-repo with different branch and app", func() {
		tip := generateTestInputs()
		tip2 := generateTestInputs()
		appName := tip.appRepoName
		appName2 := tip2.appRepoName
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + ".git"
		prLink := "https://" + gitProviderName + ".com/" + gitOrg + "/" + tip.appRepoName + "/pull/1"

		addCommand := "add app . --name=" + appName
		addCommand2 := "add app . --name=" + appName2

		defer deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitProvider, gitOrg)
		})

		By("When I create an empty private repo for app", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitProvider, true, gitOrg)
		})

		By("And I git add-commit-push for app with workload", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And I run add app command for "+appName, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see a PR created for "+appName, func() {
			verifyPRCreated(repoAbsolutePath, appName, gitProvider)
		})

		By("And I should fail to create a PR with the same app repo consecutively", func() {
			_, addCommandErr := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
			if os.Getenv("GIT_PROVIDER") == "gitlab" {
				Expect(addCommandErr).Should(ContainSubstring("400 {message: Branch already exists}"))
			} else {
				Expect(addCommandErr).Should(ContainSubstring("422 Reference already exists"))
			}
		})

		By("When I merge the previous PR", func() {
			mergePR(repoAbsolutePath, prLink, gitProvider)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And I should fail to create another PR with the same app", func() {
			_, addCommandErr := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
			Expect(addCommandErr).Should(ContainSubstring("unable to create resource, resource already exists in cluster"))
		})
	})

	It("@skipOnNightly SmokeTestLong - Verify that gitops can deploy a single workload to multiple clusters with app manifests in config repo (Bug #810)", func() {
		var appRemoveOutput string
		tip := generateTestInputs()
		appRepoName := "test-app-" + RandString(8)
		appName := appRepoName
		appRepoRemoteURL := "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appRepoName + ".git"

		addCommand := "add app . --name=" + appName + " --auto-merge=true"

		cluster1Context := clusterContext
		cluster2Name, cluster2Context, err := ResetOrCreateClusterWithName(WEGO_DEFAULT_NAMESPACE, deleteWegoRuntime, "", true)
		Expect(err).ShouldNot(HaveOccurred())

		defer deleteRepo(appRepoName, gitProvider, gitOrg)
		defer func() {
			selectCluster(cluster1Context)
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
			deleteCluster(cluster2Name)
		}()

		By("And application repos do not already exist", func() {
			deleteRepo(appRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to clusters", func() {
			selectCluster(cluster1Context)
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
			selectCluster(cluster2Context)
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("And I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitProvider, true, gitOrg)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And Gitops runtime is not installed in Cluster2", func() {
			selectCluster(cluster2Context)
			uninstallWegoRuntime(WEGO_DEFAULT_NAMESPACE)
		})

		By("And gitops check pre kubernetes version is compatible and flux is not installed in Cluster2", func() {
			selectCluster(cluster2Context)
			c := exec.Command(gitopsBinaryPath, "check", "--pre")
			output, err := c.CombinedOutput()
			Expect(err).NotTo(HaveOccurred())
			expectedOutput := fmt.Sprintf(`✔ Kubernetes %s >=[0-9]+.[0-9]+\.[0-9]+-[0-9]+
✔ Flux is not installed
`,
				getK8sVersion())
			Expect(string(output)).To(MatchRegexp(expectedOutput))
		})

		By("And I install gitops to my active clusters", func() {
			selectCluster(cluster1Context)
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
			selectCluster(cluster2Context)
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, appRepoRemoteURL)
		})

		By("And the kustomizations should not have any errors", func() {
			selectCluster(cluster1Context)
			VerifyKustomizations(cluster1Context, WEGO_DEFAULT_NAMESPACE)
			selectCluster(cluster2Context)
			VerifyKustomizations(cluster2Context, WEGO_DEFAULT_NAMESPACE)
		})

		By("And gitops check pre validates kubernetes and flux compatibility for Cluster1", func() {
			selectCluster(cluster1Context)
			c := exec.Command(gitopsBinaryPath, "check", "--pre")
			actualOutput, err := c.CombinedOutput()
			Expect(err).NotTo(HaveOccurred())
			fluxVersion, err := getCurrentFluxSupportedVersion()
			Expect(err).ShouldNot(HaveOccurred())
			expectedOutput := fmt.Sprintf(`✔ Kubernetes %s >=[0-9]+.[0-9]+\.[0-9]+-[0-9]+
✔ Flux %s ~=%s
%s
`,
				getK8sVersion(),
				fluxVersion, fluxVersion,
				check.FluxCompatibleMessage)
			Expect(string(actualOutput)).To(MatchRegexp(expectedOutput))
		})

		By("And I run gitops add command for app", func() {
			selectCluster(cluster1Context)
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
			selectCluster(cluster2Context)
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			selectCluster(cluster1Context)
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			selectCluster(cluster2Context)
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I should see workload for app is deployed to the clusters", func() {
			selectCluster(cluster1Context)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
			selectCluster(cluster2Context)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("When I delete the app on cluster1", func() {
			selectCluster(cluster1Context)
			appRemoveOutput, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s delete app %s --auto-merge", gitopsBinaryPath, appName))
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(MatchRegexp(`► Removing application "` + appName + `" from cluster .* and repository`))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(ContainSubstring("► Pushing app changes to repository"))
		})

		By("And app should get deleted from cluster1", func() {
			err = waitForAppRemoval(appName)
			Expect(err).NotTo(HaveOccurred())
		})

		By("And app in cluster2 should keep running", func() {
			selectCluster(cluster2Context)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

})

var _ = Describe("Weave GitOps Add Tests With Long Cluster Name", func() {
	deleteWegoRuntime := false
	if os.Getenv("DELETE_WEGO_RUNTIME_ON_EACH_TEST") == "true" {
		deleteWegoRuntime = true
	}

	var _ = BeforeEach(func() {

		By("Given I have a brand new cluster with a long cluster name", func() {
			var err error

			clusterName = "kind-123456789012345678901234567890"
			_, clusterContext, err = ResetOrCreateClusterWithName(WEGO_DEFAULT_NAMESPACE, deleteWegoRuntime, clusterName, false)
			Expect(err).ShouldNot(HaveOccurred())
		})

		By("And I have a gitops binary installed on my local machine", func() {
			Expect(FileExists(gitopsBinaryPath)).To(BeTrue())
		})
	})

	It("SmokeTestLong - Verify that gitops can deploy an app with long name", func() {
		var configRepoRemoteURL string
		var listOutput string
		var appStatus string
		private := true
		readmeFilePath := "./data/README.md"
		tip := generateTestInputs()
		appFilesRepoName := tip.appRepoName + "123456789012345678901234567890"
		appConfigRepoName := "config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@" + gitProviderName + ".com/" + gitOrg + "/" + appConfigRepoName + ".git"
		appName := appFilesRepoName
		workloadName := tip.workloadName
		workloadNamespace := tip.workloadNamespace
		appManifestFilePath := tip.appManifestFilePath

		addCommand := "add app . --auto-merge=true"

		defer deleteRepo(appFilesRepoName, gitProvider, gitOrg)
		defer deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		defer deleteWorkload(workloadName, workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(appFilesRepoName, gitProvider, gitOrg)
			deleteRepo(appConfigRepoName, gitProvider, gitOrg)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(workloadName, workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("When I create a private repo with app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appFilesRepoName, gitProvider, private, gitOrg)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE, configRepoRemoteURL)
		})

		By("And I run gitops add app command for app: "+appName, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(workloadName, workloadNamespace)
		})

		By("When I check the app status for app", func() {
			appStatus, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get app " + appName)
		})

		By("Then I should see the status for "+appName, func() {
			Eventually(appStatus).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus).Should(ContainSubstring(`gitrepository/` + appName))
			Eventually(appStatus).Should(ContainSubstring(`kustomization/` + appName))
		})

		By("When I check for apps", func() {
			listOutput, _ = runCommandAndReturnStringOutput(gitopsBinaryPath + " get apps")
		})

		By("Then I should see app list contain appName", func() {
			Eventually(listOutput).Should(ContainSubstring(appName))
		})

		By("And I should not see gitops components in app repo: "+appFilesRepoName, func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".weave-gitops"))
		})

		By("And I should see gitops components in config repo: "+appConfigRepoName, func() {
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && git clone %s && cd %s && ls -al", repoAbsolutePath, configRepoRemoteURL, appConfigRepoName))
			Expect(folderOutput).Should(ContainSubstring(".weave-gitops"))
		})
	})
})
