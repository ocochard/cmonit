# Monit Collector Protocol Documentation

**Last Updated:** 2025-11-24
**Monit Version Tested:** 5.35.2

## Overview

Monit agents send status and event data to M/Monit (or cmonit) via HTTP POST requests to the `/collector` endpoint.

## Protocol Details

### HTTP Request Format

```
POST /collector HTTP/1.1
Host: <hostname>:<port>
Content-Type: text/xml
Content-Length: <length>
Content-Encoding: gzip (optional, if compression is enabled)
Pragma: no-cache
Accept: */*
User-Agent: Monit/<version>
Authorization: Basic <base64(username:password)>

<XML body>
```

### Configuration in monitrc

```
set mmonit http://username:password@hostname:port/collector
```

Example from current system:
```
set mmonit http://monit:monit@127.0.0.1:8080/collector
```

### Authentication

- **Method**: HTTP Basic Authentication
- Username and password are embedded in the URL
- Credentials are base64-encoded in the Authorization header

### Compression

- Monit can optionally compress the XML body using gzip
- The collector response header `Server: mmonit/` is checked to determine compression support
- If M/Monit version >= 3.6, compression is enabled
- When compressed, the header `Content-Encoding: gzip` is added

### XML Data Format

The XML body contains the monit status:

**Important XML Structure Notes (Monit 5.35.2):**
- Services are wrapped in a `<services>` container element
- Service `name` is an **attribute**: `<service name="foo">`
- Service `type` is an **element**: `<type>5</type>` (not an attribute)

```xml
<?xml version="1.0" encoding="ISO-8859-1"?>
<monit id="<unique-id>" incarnation="<timestamp>" version="<monit-version>">
  <server>
    <uptime><seconds></uptime>
    <poll><seconds></poll>
    <startdelay><seconds></startdelay>
    <localhostname><hostname></localhostname>
    <controlfile><path></controlfile>
    <httpd>
      <address><ip></address>
      <port><port></port>
      <ssl><0|1></ssl>
    </httpd>
    <credentials>
      <username><username></username>
      <password><password></password>
    </credentials>
  </server>
  <platform>
    <name><os-name></name>
    <release><os-release></release>
    <version><os-version></version>
    <machine><architecture></machine>
    <cpu><cpu-count></cpu>
    <memory><bytes></memory>
    <swap><bytes></swap>
  </platform>
  <services>
    <service name="<service-name>">
      <type><0-9></type>
      <collected_sec><unix-timestamp></collected_sec>
      <collected_usec><microseconds></collected_usec>
      <status><0-9></status>
      <status_hint><0-1></status_hint>
      <monitor><0-2></monitor>
      <monitormode><0-2></monitormode>
      <onreboot><0-3></onreboot>
      <pendingaction><0-10></pendingaction>

      <!-- System service specific fields -->
      <system>
        <load>
          <avg01><value></avg01>
          <avg05><value></avg05>
          <avg15><value></avg15>
        </load>
        <cpu>
          <user><percentage></user>
          <system><percentage></system>
          <nice><percentage></nice>
          <wait><percentage></wait>
          ...
        </cpu>
        <memory>
          <percent><value></percent>
          <kilobyte><value></kilobyte>
        </memory>
        <swap>
          <percent><value></percent>
          <kilobyte><value></kilobyte>
        </swap>
      </system>

      <!-- Process service specific fields (Type 3) -->
      <pid><pid></pid>
      <ppid><ppid></ppid>
      <uid><uid></uid>
      <euid><euid></euid>
      <gid><gid></gid>
      <uptime><seconds></uptime>
      <threads><count></threads>
      <children><count></children>
      <memory>
        <percent><value></percent>
        <percenttotal><value></percenttotal>
        <kilobyte><value></kilobyte>
        <kilobytetotal><value></kilobytetotal>
      </memory>
      <cpu>
        <percent><value></percent>
        <percenttotal><value></percenttotal>
      </cpu>
      <filedescriptors>
        <open><count></open>
        <opentotal><count></opentotal>
        <limit>
          <soft><limit></soft>
          <hard><limit></hard>
        </limit>
      </filedescriptors>
      <read>
        <operations>
          <count><count></count>
          <total><total></total>
        </operations>
      </read>
      <write>
        <operations>
          <count><count></count>
          <total><total></total>
        </operations>
      </write>

      <!-- Filesystem service specific fields (Type 0) -->
      <fstype><type></fstype>
      <fsflags><flags></fsflags>
      <mode><octal-mode></mode>
      <block>
        <percent><value></percent>
        <usage><gigabytes></usage>
        <total><gigabytes></total>
      </block>
      <inode>
        <percent><value></percent>
        <usage><count></usage>
        <total><count></total>
      </inode>
      <read>
        <bytes>
          <count><count></count>
          <total><total></total>
        </bytes>
        <operations>
          <count><count></count>
          <total><total></total>
        </operations>
      </read>
      <write>
        <bytes>
          <count><count></count>
          <total><total></total>
        </bytes>
        <operations>
          <count><count></count>
          <total><total></total>
        </operations>
      </write>

      <!-- File service specific fields (Type 2) -->
      <mode><octal-mode></mode>
      <uid><uid></uid>
      <gid><gid></gid>
      <timestamps>
        <access><unix-timestamp></access>
        <change><unix-timestamp></change>
        <modify><unix-timestamp></modify>
      </timestamps>
      <size><bytes></size>
      <hardlink><count></hardlink>
      <checksum type="MD5"><hash></checksum>

      <!-- Program service specific fields (Type 7) -->
      <program>
        <started><unix-timestamp></started>
        <status><exit-code></status>
        <output><![CDATA[program output]]></output>
      </program>

      <!-- Network interface service specific fields (Type 8) -->
      <link>
        <state><0|1></state>
        <speed><bits-per-second></speed>
        <duplex><0|1></duplex>
        <download>
          <packets>
            <now><count></now>
            <total><count></total>
          </packets>
          <bytes>
            <now><bytes></now>
            <total><bytes></total>
          </bytes>
          <errors>
            <now><count></now>
            <total><count></total>
          </errors>
        </download>
        <upload>
          <packets>
            <now><count></now>
            <total><count></total>
          </packets>
          <bytes>
            <now><bytes></now>
            <total><bytes></total>
          </bytes>
          <errors>
            <now><count></now>
            <total><count></total>
          </errors>
        </upload>
      </link>

      <!-- Port/connection checks (Type 6) -->
      <port>
        <hostname><hostname></hostname>
        <portnumber><port></portnumber>
        <request><![CDATA[request]]></request>
        <protocol><name></protocol>
        <type><tcp|udp></type>
        <responsetime><seconds></responsetime>
      </port>

    </service>
    ...
  </services>
  <servicegroups>
    <servicegroup name="<group-name>">
      <service><name></service>
      ...
    </servicegroup>
    ...
  </servicegroups>
  <event>
    <!-- Only present if this is an event notification -->
  </event>
</monit>
```

### Response

The collector should respond with:

```
HTTP/1.1 200 OK
Server: mmonit/<version>
Content-Length: 0

```

- Status code 200-299 indicates success
- Status code >= 400 indicates error
- The `Server:` header should include "mmonit/<version>" to enable compression in future requests

## Source Code References

From monit-5.35.2 source:

- **Collector interface**: `src/notification/MMonit.c` - Handles sending data to M/Monit
- **XML generation**: `src/http/xml.c` - Generates the XML status/event messages
- **Main function**: `MMonit_send()` at line 151 of MMonit.c
- **XML builder**: `status_xml()` at line 635 of xml.c

## Current Test Setup

- Monit is running on the system
- Configuration: `/usr/local/etc/monitrc`
- Collector URL: `http://monit:monit@127.0.0.1:8080/collector`
- Monitored services:
  - System (CPU, memory, swap, load)
  - sshd process
  - nginx process
  - File monitoring (SSL certificates)
  - Custom program check (temperature)

## Data Collection Frequency

- Default check interval: 30 seconds
- Configurable with `set daemon <seconds>`
- Status is pushed to collector on each check cycle
- Events are sent immediately when state changes
