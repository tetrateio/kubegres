#!/bin/bash
set -ex

dt=$(date '+%d/%m/%Y %H:%M:%S');
echo "$dt - Attempting to copy Primary DB to Replica DB...";

ssl_ca_file="{{ .RootCertPath }}"
ssl_cert_file="{{ .CertPath }}"
ssl_key_file="{{ .KeyPath }}"

SSL_MODE=${SSL_MODE:-verify-ca}

if [ -z "$(ls -A $PGDATA)" ]; then
  # If the DB directory is empty, assume this is the first time the container is started
  # and perform the initial backup and the replication setup

  connection_string="sslmode=${SSL_MODE} sslrootcert=$ssl_ca_file sslcert=$ssl_cert_file sslkey=$ssl_key_file host=$PRIMARY_HOST_NAME user=replication"


  echo "$dt - Copying Primary DB to Replica DB folder: $PGDATA";
  echo "$dt - Running: pg_basebackup -R --dbname \"$connection_string\" -D $PGDATA -P ;";


  pg_basebackup -R --dbname "$connection_string" -D $PGDATA -P ;

  if [ ${UID:-1} -eq 0 ]; then
    chown -R postgres:postgres $PGDATA;
  fi

  echo "$dt - Copy completed";

else
  # The 'primary_conninfo' parameter is set by the 'pg_basebackup' command in the postgresql.auto.conf file and we cannot assume the exact order of the parameters.
  # Let's pipe grep commands for each parameter

  ssl_mode_args="sslmode=''${SSL_MODE}''"
  ssl_cert_args="sslcert=''$ssl_cert_file'' sslkey=''$ssl_key_file'' sslrootcert=''$ssl_ca_file''"
  host_args="host=$PRIMARY_HOST_NAME"
  auth_args="user=replication password=$PGPASSWORD"

  # The `primary_conninfo` parameter is set by the `pg_basebackup` command in the postgresql.auto.conf file and we cannot assume the exact order of the parameters.
  # Let's pipe grep commands for each parameter
  primary_already_set=$(grep "$host_args" $PGDATA/postgresql.auto.conf | grep "$auth_args" | grep "$ssl_mode_args" | grep "$ssl_cert_args" > /dev/null; echo $?)

  if [ $primary_already_set -ne 0 ]; then
    echo "$dt - Updating primary_conninfo in postgresql.auto.conf to connect to $PRIMARY_HOST_NAME using TLS";
    primary_conninfo="$ssl_mode_args $ssl_cert_args $host_args $auth_args"
    echo "$dt - primary_conninfo: $primary_conninfo";
    echo "primary_conninfo = '$primary_conninfo'" > $PGDATA/postgresql.auto.conf
  fi

  echo "$dt - Skipping copy from Primary DB because Replica DB already exists";
fi
