package app

import (
	"fmt"
	"strings"
)

type KubernetesBackend struct {
	config    *Configuration
	executor  *CommandExecutor
	namespace string
	podCache  map[string]string
}

func NewKubernetesBackend(config *Configuration, executor *CommandExecutor) *KubernetesBackend {
	return &KubernetesBackend{config: config, executor: executor, podCache: map[string]string{}}
}

func (k *KubernetesBackend) Name() string {
	return "kubernetes"
}

func (k *KubernetesBackend) Info() string {
	return k.namespace
}

func (k *KubernetesBackend) Detect() error {
	if err := k.executor.runCommandQuiet("kubectl", "version", "--client"); err != nil {
		return fmt.Errorf("kubectl CLI not available: %w", err)
	}

	namespaces, err := ListKubernetesNamespaces(k.executor)
	if err != nil {
		return err
	}

	if k.config.K8sNamespace != "" {
		k.namespace = k.config.K8sNamespace
		if _, err := k.executor.runCommand("kubectl", "get", "pods", "-n", k.namespace, "-l", "app.kubernetes.io/name=infrahub"); err != nil {
			return fmt.Errorf("failed to verify namespace %s: %w", k.namespace, err)
		}
		return nil
	}

	switch len(namespaces) {
	case 0:
		return ErrEnvironmentNotFound
	case 1:
		k.namespace = namespaces[0]
		k.config.K8sNamespace = k.namespace
		return nil
	default:
		return fmt.Errorf("multiple kubernetes namespaces found: %s (set INFRAHUB_K8S_NAMESPACE)", strings.Join(namespaces, ", "))
	}
}

func (k *KubernetesBackend) Exec(service string, command []string, opts *ExecOptions) (string, error) {
	pod, err := k.getPodForService(service)
	if err != nil {
		return "", err
	}
	finalCmd := k.prepareCommand(command, opts)
	args := []string{"exec", "-n", k.namespace, pod, "--"}
	args = append(args, finalCmd...)
	return k.executor.runCommand("kubectl", args...)
}

func (k *KubernetesBackend) ExecStream(service string, command []string, opts *ExecOptions) (string, error) {
	pod, err := k.getPodForService(service)
	if err != nil {
		return "", err
	}
	finalCmd := k.prepareCommand(command, opts)
	args := []string{"exec", "-n", k.namespace, pod, "--"}
	args = append(args, finalCmd...)
	return k.executor.runCommandWithStream("kubectl", args...)
}

func (k *KubernetesBackend) CopyTo(service, src, dest string) error {
	pod, err := k.getPodForService(service)
	if err != nil {
		return err
	}
	target := fmt.Sprintf("%s/%s:%s", k.namespace, pod, dest)
	if _, err := k.executor.runCommand("kubectl", "cp", src, target); err != nil {
		return err
	}
	return nil
}

func (k *KubernetesBackend) CopyFrom(service, src, dest string) error {
	pod, err := k.getPodForService(service)
	if err != nil {
		return err
	}
	source := fmt.Sprintf("%s/%s:%s", k.namespace, pod, src)
	if _, err := k.executor.runCommand("kubectl", "cp", source, dest); err != nil {
		return err
	}
	return nil
}

func (k *KubernetesBackend) Start(services ...string) error {
	return k.scaleServices(services, 1)
}

func (k *KubernetesBackend) Stop(services ...string) error {
	return k.scaleServices(services, 0)
}

func (k *KubernetesBackend) IsRunning(service string) (bool, error) {
	statuses, err := k.getPodStatuses(service)
	if err != nil {
		return false, err
	}
	for _, status := range statuses {
		if strings.EqualFold(status, "Running") {
			return true, nil
		}
	}
	return false, nil
}


func (k *KubernetesBackend) getPodStatuses(service string) ([]string, error) {
	selectors := k.podSelectors(service)
	for _, selector := range selectors {
		output, err := k.executor.runCommand("kubectl", "get", "pods", "-n", k.namespace, "-l", selector, "-o", "jsonpath={range .items[*]}{.status.phase}{\"\\n\"}{end}")
		if err != nil {
			continue
		}
		statuses := nonEmptyLines(output)
		if len(statuses) > 0 {
			return statuses, nil
		}
	}
	// Fallback to all pods search
	output, err := k.executor.runCommand("kubectl", "get", "pods", "-n", k.namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}{\";\"}{.status.phase}{\"\\n\"}{end}")
	if err != nil {
		return nil, err
	}
	statuses := []string{}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) != 2 {
			continue
		}
		if strings.Contains(parts[0], service) {
			statuses = append(statuses, parts[1])
		}
	}
	return statuses, nil
}

func (k *KubernetesBackend) getPodForService(service string) (string, error) {
	if pod, ok := k.podCache[service]; ok && pod != "" {
		return pod, nil
	}

	selectors := k.podSelectors(service)
	for _, selector := range selectors {
		output, err := k.executor.runCommand("kubectl", "get", "pods", "-n", k.namespace, "-l", selector, "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
		if err != nil {
			continue
		}
		pods := nonEmptyLines(output)
		if len(pods) > 0 {
			k.podCache[service] = pods[0]
			return pods[0], nil
		}
	}

	output, err := k.executor.runCommand("kubectl", "get", "pods", "-n", k.namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
	if err != nil {
		return "", err
	}
	for _, name := range nonEmptyLines(output) {
		if strings.Contains(name, service) {
			k.podCache[service] = name
			return name, nil
		}
	}

	return "", fmt.Errorf("no pods found for service %s in namespace %s", service, k.namespace)
}
