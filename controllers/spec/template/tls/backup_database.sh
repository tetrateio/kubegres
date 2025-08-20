#!/bin/bash
set -e

dt=$(date '+%d/%m/%Y %H:%M:%S');
fileDt=$(date '+%d_%m_%Y_%H_%M_%S');
backUpFileName="$KUBEGRES_RESOURCE_NAME-backup-$fileDt.gz"
backUpFilePath="$BACKUP_DESTINATION_FOLDER/$backUpFileName"

ssl_ca_file="{{ .RootCertPath }}"
ssl_cert_file="{{ .CertPath }}"
ssl_key_file="{{ .KeyPath }}"

SSL_MODE=${SSL_MODE:-verify-ca}
POSTGRES_USER=${POSTGRES_USER:-postgres}

connection_string="sslmode=${SSL_MODE} sslrootcert=$ssl_ca_file sslcert=$ssl_cert_file sslkey=$ssl_key_file host=$BACKUP_SOURCE_DB_HOST_NAME user=$POSTGRES_USER"

echo "$dt - Starting DB backup of Kubegres resource $KUBEGRES_RESOURCE_NAME into file: $backUpFilePath";

tempdump=$(mktemp)

echo -e "$dt - Running:\n\tpg_dumpall --dbname \"$connection_string\" -c > $tempdump;\n\tgzip -c $tempdump > $backUpFilePath"

pg_dumpall --dbname "$connection_string" -c > $tempdump

if [ $? -ne 0 ]; then
  rm $tempdump
  echo "Unable to execute a BackUp. Please check DB connection settings"
  exit 1
fi

gzip -c $tempdump > $backUpFilePath

if [ $? -ne 0 ]; then
  rm $tempdump
  rm $backUpFilePath
  echo "Unable to execute a BackUp. Please check DB connection settings"
  exit 1
fi

echo "$dt - DB backup completed for Kubegres resource $KUBEGRES_RESOURCE_NAME into file: $backUpFilePath";
