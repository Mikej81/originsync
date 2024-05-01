# OriginSync

## Overview

OriginSync is a Kubernetes watcher application that monitors services within a Kubernetes cluster and manages origin pools based on service deployment and updates. It's designed to integrate seamlessly with Kubernetes, using the Kubernetes API to watch for changes in service configurations and dynamically update origin pools accordingly.

## Features

- **Service Monitoring:** Watches for creation, updates, and deletion of Kubernetes services.
- **Dynamic Configuration:** Automatically manages origin pools based on the service's status and specifications.
- **Efficient Resource Handling:** Uses Kubernetes Informers and Watches to efficiently listen for changes without polling.

## Prerequisites

Before you begin, ensure you meet the following requirements:

- Kubernetes cluster
- kubectl configured with access to your cluster
- Permission to create, update, and delete services in the cluster
- Distributed Cloud Tenant
- Distributed Cloud API Token
- An Application Namespace for the Origin Pools

## Installation

```bash
kubectl apply -f deploy/
```

## Configuration

OriginSync uses the following environment variables for configuration:

- KUBE_NAMESPACE: Optional. Specify the namespace to watch. If not set, all namespaces are watched.
- XC_NAMESPACE, XC_TOKEN, API_DOMAIN: Required for API interaction.

Set these in the deployment YAML under env:

```yaml
env:
  - name: KUBE_NAMESPACE
    value: "default"  # Example: Watch the 'default' namespace only
  - name: XC_NAMESPACE
    value: "your-xc-namespace"
  - name: XC_TOKEN
    value: "your-xc-token"
  - name: API_DOMAIN
    value: "https://your-api-domain.com"
```
