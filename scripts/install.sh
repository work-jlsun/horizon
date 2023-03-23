#!/bin/bash

VERSION="2.1.4"
CHINA=false
FULL=false
MINIKUBE=false
KIND=false

# Install horizon of the script
#
# Pre-requisites:
# - kubectl
# - helm
# - docker
# - kind or minikube

# Check if the binary is installed
# If not, return false, else return true
function checkbinary() {
    if command -v $1 &> /dev/null
    then
        echo "The binary $1 is installed"
        return 0
    else
        echo "The binary $1 is not installed"
        return 1
    fi
}

function mustinstalled() {
    if ! command -v $1 &> /dev/null
    then
        echo "The binary $1 is not installed"
        exit
    else
        echo "The binary $1 is installed"
    fi
}

function installhelm() {
    echo "Installing helm"
    curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
}

function installkubectl() {
    echo "Installing kubectl"
    curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.21.0/bin/linux/amd64/kubectl
    chmod +x ./kubectl
    sudo mv ./kubectl /usr/local/bin/kubectl
}

function installkind() {
    echo "Installing kind"
    curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.11.1/kind-linux-amd64
    chmod +x ./kind
    sudo mv ./kind /usr/local/bin/kind
}

function installminikube() {
    echo "Installing minikube"
    curl -Lo minikube https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
    chmod +x minikube
    sudo mv minikube /usr/local/bin/
}

function checkprerequesites() {
    mustinstalled docker

    # If kind is not installed, install kind
    if ! checkbinary kind
    then
        installkind
    fi

    # If kubectl is not installed, install kubectl
    if ! checkbinary kubectl
    then
        installkubectl
    fi

    # If helm is not installed, install helm
    if ! checkbinary helm
    then
        installhelm
    fi
}

function cmdhelp() {
    echo "Usage: $0 [options]"
    echo "Options:"
    echo "  -h, --help"
    echo "  -k, --kind"
    echo "  -m, --minikube"
    echo "  -c, --clean"
    echo "  -v, --version <VERSION>"
    echo "  -f, --full"
    # install for user from China
    echo "  -cn, --china"
}

function kindcreatecluster() {
    echo "Creating kind cluster"

echo \
'kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 80
        protocol: TCP
      - containerPort: 443
        hostPort: 443
        protocol: TCP' > /tmp/kind.yaml


    kind create cluster --image=kindest/node:v1.19.16 --name=horizon --config=/tmp/kind.yaml

    docker exec horizon-control-plane bash -c \
        $'echo "nameserver `kubectl get service -n kube-system kube-dns -o jsonpath=\'{.spec.clusterIP}\'`" >> /etc/resolv.conf'

    docker exec horizon-control-plane bash -c \
$'echo \'[plugins."io.containerd.grpc.v1.cri".registry.configs."harbor.horizoncd.svc.cluster.local".tls]
  insecure_skip_verify = true\' >> /etc/containerd/config.toml'

    docker exec horizon-control-plane systemctl restart containerd

    kubectl config use-context kind-horizon
}

function minikubecreatecluster() {
    echo "Creating minikube cluster"
    minikube start --container-runtime=docker --driver=docker \
        --kubernetes-version=v1.19.16 --cpus=4 --memory=8000 --ports=80:80 --ports=443:443

    kubectl get service -n kube-system kube-dns -o jsonpath='{.spec.clusterIP}' | xargs \
        -I {} docker exec minikube bash -c 'echo "nameserver {}" >> /etc/resolv.conf'

    kubectl config use-context minikube
}

function installingress() {
    # install ingress-nginx by helm
    echo "Installing ingress-nginx"
    helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
    helm install my-ingress-nginx -n ingress-nginx ingress-nginx/ingress-nginx \
        --version 4.1.4 --set controller.hostNetwork=true --set controller.watchIngressWithoutClass=true --create-namespace

    # wait for ingress-nginx to be ready
    echo "Waiting for ingress-nginx to be ready"
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=my-ingress-nginx,app.kubernetes.io/name=ingress-nginx --timeout=600s -n ingress-nginx
}

# print progress bar by count of how many pods was ready
# 'kubectl get pods -n horizoncd'
function progressbar() {
 # 获取pods的总数
  total=$(kubectl get pods -nhorizoncd --field-selector=status.phase!=Failed 2> /dev/null | grep -v NAME | wc -l | awk '{print $1}')

  while true; do
    # 获取ready的pods的个数
    ready=$(kubectl get pods -nhorizoncd --field-selector=status.phase=Running 2> /dev/null | \
      awk '{print $2}' | grep -v READY | awk -F/ '$1 == $2 {print}' | wc -l)
    completed=$(kubectl get pods -nhorizoncd --field-selector=status.phase=Succeeded 2> /dev/null | \
                   grep -v NAME -c)
    ready=$((ready + completed))

    # 计算进度条的长度
    bar_length=50
    completed=$((ready * bar_length / total))

    # 输出进度条
    echo -ne '['
    for ((i=0; i<$bar_length; i++)); do
      if ((i < $completed)); then
        echo -ne '#'
      else
        echo -ne '-'
      fi
    done
    echo -ne "] (${ready}/${total})\r"

    # 暂停1秒钟，然后清除当前行
    sleep 1
    echo -ne "\033[K"

    # 如果所有pods都已经ready，则退出循环
    if ((ready == total)); then
      break
    fi
  done

  echo "Horizon is installed"
}

function install() {

    helm repo add horizon https://horizoncd.github.io/helm-charts

    cmd="helm install horizon horizon/horizon -n horizoncd --version $VERSION --create-namespace --timeout 60s"
    if $CHINA
    then
        cmd="$cmd -f https://raw.githubusercontent.com/horizoncd/helm-charts/main/horizon-cn-values.yaml"
    fi

    eval $cmd 2> /dev/null

    progressbar
}

function clean() {
#    echo "Cleaning horizon"
#    helm uninstall horizon -n horizoncd
#    kubectl delete ns horizoncd
#
#    echo "Cleaning ingress-nginx"
#    helm uninstall my-ingress-nginx -n ingress-nginx
#    kubectl delete ns ingress-nginx

    echo "Cleaning kind cluster"
    kind delete cluster --name horizon || minikube delete
}

function applyinitjobtok8s(){
    echo "Applying init job to k8s"

    image="horizoncd/init:v1.0.0"
    if $CHINA
    then
        image="registry.cn-hangzhou.aliyuncs.com/horizoncd/horizoncd.init:v1.0.0"
    fi

    INDENT="    "
    if $KIND 
    then
      kubeconfig=$(docker exec horizon-control-plane cat /etc/kubernetes/admin.conf | sed "2,\$s/^/$INDENT/")
    elif
    then
      kubeconfig=$(docker exec minikube cat /etc/kubernetes/admin.conf | sed "2,\$s/^/$INDENT/")
    fi

    cat <<EOF | kubectl apply -nhorizoncd -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: horizon-init
data:
  script: |
    import subprocess
    import requests
    import pymysql
    import os

    host = os.environ.get('MYSQL_HOST', '127.0.0.1')
    port = os.environ.get('MYSQL_PORT', '3306')
    username = os.environ.get('MYSQL_USER', 'root')
    password = os.environ.get('MYSQL_PASSWORD', '123456')
    db = os.environ.get('MYSQL_DATABASE', 'horizon')

    connection = pymysql.connect(host=host, user=username,
                                password=password, database=db, port=int(port), cursorclass=pymysql.cursors.DictCursor)

    sql_registry = "insert into tb_registry (id, name, server, token, path, insecure_skip_tls_verify, kind) VALUES (1, 'local', 'https://horizon-registry.horizoncd.svc.cluster.local', 'YWRtaW46SGFyYm9yMTIzNDU=', 'library', 1, 'harbor')"
    sql_kubernetes = '''INSERT INTO tb_region (id, name, display_name, server, certificate, ingress_domain, prometheus_url, disabled, registry_id) VALUES (1, 'local', 'local','https://kubernetes.default.svc', '$kubeconfig','', '', 0, 1)'''

    sql_tag = "INSERT INTO tb_tag (id, resource_id, resource_type, tag_key, tag_value) VALUES (1, 1, 'regions', 'cloudnative-kubernetes-groups', 'public')"
    sql_environment = "INSERT INTO tb_environment (id, name, display_name, auto_free) VALUES (1, 'local', 'local', 0)"
    sql_environment_region = "INSERT INTO tb_environment_region (id, environment_name, region_name, is_default, disabled) VALUES (1, 'local', 'local', 0, 0)"
    sql_group = "INSERT INTO tb_group (id, name, path, description, visibility_level, parent_id, traversal_ids, region_selector) VALUES (1,'horizon', 'horizon', '', 'private', 0, 1, '- key: cloudnative-kubernetes-groups\n  values:\n    - public\n  operator: ""')"
    sql_template = "INSERT INTO tb_template (id, name, description, repository, group_id, chart_name, only_admin, only_owner, without_ci) VALUES (1, 'deployment', '', 'https://github.com/horizoncd/deployment.git', 0, 'deployment', 0, 0, 1)"
    sql_template_release = "INSERT INTO tb_template_release (id, template_name, name, description, recommended, template, chart_name, only_admin, chart_version, sync_status, failed_reason, commit_id, last_sync_at, only_owner) VALUES (1, 'deployment', 'v0.0.1', '', 1, 1, 'deployment', 0, 'v0.0.1-5e5193b355961b983cab05a83fa22934001ddf4d', 'status_succeed', '', '5e5193b355961b983cab05a83fa22934001ddf4d', '2023-03-22 17:28:38', 0)"

    sqls = [sql_registry, sql_kubernetes, sql_tag, sql_environment,
            sql_environment_region, sql_group, sql_template, sql_template_release]

    with connection:
        with connection.cursor() as cursor:
            for sql in sqls:
                try:
                    cursor.execute(sql)
                except Exception as e:
                    print("Error:", e)
                    print("sql:", sql)
        connection.commit()

    user = "horizoncd"
    repo = "deployment"
    format = "tarball"  # 或者 "tarball"
    branch = "main"   # 或者其他分支名称
    url = f"https://github.com/{user}/{repo}/{format}/{branch}"
    response = requests.get(url, stream=True)
    chart_file_path = "/tmp/deployment.tgz"
    with open(chart_file_path, "wb") as f:
        for chunk in response.iter_content(chunk_size=1024):
            if chunk:
                f.write(chunk)
    chartmuseum_url = os.environ.get("CHARTMUSEUM_URL", "http://localhost:8080")
    version = "v0.0.1"
    command = ["helm", "cm-push", "--version", version, chart_file_path, chartmuseum_url]
    result = subprocess.run(command, shell=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode == 0:
        print("Chart包上传成功！")
    else:
        print(f"Chart包上传失败：{result.stderr.decode('utf-8')}")
        exit(1)

---
apiVersion: batch/v1
kind: Job
metadata:
  name: horizon-init
spec:
  template:
    spec:
      containers:
      - name: init
        image: $image
        command: ["python","/init/script.py"]
        env:
          - name: MYSQL_HOST
            value: "horizon-mysql.horizoncd.svc.cluster.local"
          - name: MYSQL_PORT
            value: "3306"
          - name: MYSQL_USER
            value: "root"
          - name: MYSQL_PASSWORD
            value: "horizon"
          - name: MYSQL_DATABASE
            value: "horizon"
          - name: CHARTMUSEUM_URL
            value: "http://horizon-chartmuseum.horizoncd.svc.cluster.local:8080"
        volumeMounts:
          - name: init-script
            mountPath: /init
      restartPolicy: Never

      volumes:
        - name: init-script
          configMap:
            name: horizon-init
            items:
              - key: script
                path: script.py
  backoffLimit: 1
EOF

    kubectl wait --for=condition=complete --timeout=60s -nhorizoncd job/horizon-init
}

function parseinput() {
    if [ $# -eq 0 ]
    then
        cmdhelp
        exit
    fi

    while [ $# -gt 0 ]
    do
        case $1 in
            -h|--help)
                cmdhelp
                exit
                ;;
            -k|--kind)
                KIND=true
                shift
                ;;
            -m|--minikube)
                MINIKUBE=true
                shift
                ;;
            -v|--version)
                VERSION=$2
                shift 2
                ;;
            -cn|--china)
                CHINA=true
                shift
                ;;
            -f|--full)
                FULL=true
                shift
                ;;
            -c|--clean)
                clean
                exit
                ;;
            *)
                echo "Invalid option $1"
                cmdhelp
                exit 1
                ;;
        esac
    done

     if $KIND && $MINIKUBE
     then
         echo "Cannot use both kind and minikube"
         exit
     elif ! $KIND && ! $MINIKUBE
     then
         echo "Must use either kind or minikube"
         exit
     elif $KIND
     then
         kindcreatecluster
     elif $MINIKUBE
     then
         minikubecreatecluster
     fi

     installingress

     install

     applyinitjobtok8s

     echo 'Horizon is installed successfully!'
}

parseinput "$@"
