// Package parser handles parsing of Monit XML status messages.
//
// Monit sends status updates as XML via HTTP POST to the /collector endpoint.
// This package defines Go structs that match the XML structure and provides
// functions to parse the XML into usable Go data structures.
//
// How XML Parsing Works in Go:
// 1. Define structs with fields matching the XML structure
// 2. Add struct tags to tell Go which XML element maps to which field
// 3. Use encoding/xml.Unmarshal() to parse XML bytes into the struct
//
// Example:
//   XML: <person><name>Alice</name><age>30</age></person>
//   Go struct:
//     type Person struct {
//         Name string `xml:"name"`
//         Age  int    `xml:"age"`
//     }
//   Parse: xml.Unmarshal(xmlBytes, &person)
package parser

import (
	"bytes"        // Byte slice operations
	"encoding/xml" // XML parsing and generation
	"fmt"          // Formatted I/O
	"log"          // Logging
	"os"           // Operating system functions
	"time"         // Time and date functions
)

// MonitStatus represents the complete status message from a Monit agent.
//
// This is the root element of the XML document sent by Monit.
// The XML starts with: <?xml version="1.0"?><monit>...</monit>
//
// The xml:"monit" struct tag tells Go:
// "This struct corresponds to the <monit> XML element"
//
// Struct tags are special strings after field definitions that provide
// metadata about the field. They're written like: `key:"value"`
//
// For XML parsing, the format is: `xml:"element-name"`
type MonitStatus struct {
	// XMLName is a special field that captures the XML element name
	// xml.Name is a built-in type from encoding/xml
	// This field is optional but useful for validation
	XMLName xml.Name `xml:"monit"`

	// Server contains information about the Monit agent itself
	// xml:"server" means: parse <server>...</server> into this field
	Server Server `xml:"server"`

	// Platform contains operating system information
	// xml:"platform" means: parse <platform>...</platform> into this field
	Platform Platform `xml:"platform"`

	// Services is a slice (array) of Service structs
	// xml:"service" means: parse ALL <service> elements into this slice
	// Note: Not "services" (plural), but "service" (singular)
	// Go will automatically collect all <service> elements into the slice
	//
	// Monit sends <service> elements directly under <monit>, no wrapper element
	Services []Service `xml:"service"`
}

// Server represents information about the Monit agent/daemon.
//
// Example XML:
// <server>
//   <id>abc123...</id>
//   <incarnation>1234567890</incarnation>
//   <version>5.35.2</version>
//   <hostname>web-server-01</hostname>
//   ...
// </server>
type Server struct {
	// ID is a unique identifier for this Monit instance
	// It's generated once and stays the same even if hostname changes
	// This is like a UUID for the Monit agent
	ID string `xml:"id"`

	// Incarnation is a Unix timestamp (seconds since 1970) of when Monit started
	// If Monit restarts, this changes
	// We use this to detect when a Monit agent has been restarted
	//
	// int64 is a 64-bit integer (can hold very large numbers)
	// Unix timestamps are int64 to avoid the Year 2038 problem
	Incarnation int64 `xml:"incarnation"`

	// Version is the Monit version string (e.g., "5.35.2")
	Version string `xml:"version"`

	// Uptime is how long Monit has been running (in seconds)
	Uptime int64 `xml:"uptime"`

	// Poll is the check interval in seconds (e.g., 30 = check every 30 seconds)
	Poll int `xml:"poll"`

	// StartDelay is the delay before first check (in seconds)
	// Useful to let services fully start before monitoring begins
	StartDelay int `xml:"startdelay"`

	// LocalHostname is the hostname of the monitored server
	// This is what we'll display in the UI
	LocalHostname string `xml:"localhostname"`

	// ControlFile is the path to the Monit configuration file
	// Usually /etc/monitrc or /usr/local/etc/monitrc
	ControlFile string `xml:"controlfile"`

	// HTTPD contains information about Monit's HTTP server
	// (the server that serves the Monit web UI, not our cmonit server)
	HTTPD HTTPDInfo `xml:"httpd"`

	// Credentials contains the username and password for Monit's HTTP server
	// This allows cmonit to control the Monit agent remotely
	Credentials CredentialsInfo `xml:"credentials"`
}

// HTTPDInfo represents Monit's built-in HTTP server configuration.
//
// Example XML:
// <httpd>
//   <address>::1</address>
//   <port>2812</port>
//   <ssl>0</ssl>
// </httpd>
type HTTPDInfo struct {
	// Address is the IP address Monit's HTTP server listens on
	// ::1 is IPv6 localhost, 127.0.0.1 is IPv4 localhost
	Address string `xml:"address"`

	// Port is the TCP port number (usually 2812 for Monit)
	Port int `xml:"port"`

	// SSL indicates whether Monit's HTTP server uses HTTPS
	// 0 = no SSL, 1 = SSL enabled
	SSL int `xml:"ssl"`
}

// CredentialsInfo represents Monit's HTTP server credentials.
//
// Monit sends its HTTP credentials in the XML when posting to the collector.
// We can use these credentials to control the Monit agent remotely.
//
// Example XML:
// <credentials>
//   <username>admin</username>
//   <password>monit</password>
// </credentials>
type CredentialsInfo struct {
	// Username for HTTP Basic Authentication
	Username string `xml:"username"`

	// Password for HTTP Basic Authentication
	Password string `xml:"password"`
}

// Platform represents operating system and hardware information.
//
// Example XML:
// <platform>
//   <name>FreeBSD</name>
//   <release>16.0-CURRENT</release>
//   <machine>amd64</machine>
//   <cpu>64</cpu>
//   <memory>268256896</memory>
//   <swap>41943040</swap>
// </platform>
type Platform struct {
	// Name is the OS name (Linux, FreeBSD, Darwin/macOS, etc.)
	Name string `xml:"name"`

	// Release is the OS version/release
	Release string `xml:"release"`

	// Version is the full OS version string (includes kernel info)
	Version string `xml:"version"`

	// Machine is the CPU architecture (amd64, arm64, i386, etc.)
	Machine string `xml:"machine"`

	// CPU is the number of CPU cores
	CPU int `xml:"cpu"`

	// Memory is total RAM in bytes
	// To convert to GB: memory / 1024 / 1024 / 1024
	Memory int64 `xml:"memory"`

	// Swap is total swap space in bytes
	Swap int64 `xml:"swap"`
}

// Service represents a monitored service (system, process, file, etc.).
//
// Monit can monitor different types of things:
// - Type 0: Filesystem
// - Type 1: Directory
// - Type 2: File
// - Type 3: Process (daemon/program)
// - Type 4: Remote host (ping/connection check)
// - Type 5: System (CPU, memory, load, swap)
// - Type 6: FIFO (named pipe)
// - Type 7: Program (script/command execution)
// - Type 8: Network interface
//
// Example XML:
// <service type="5">
//   <name>bigone</name>
//   <status>0</status>
//   <monitor>1</monitor>
//   <system>...</system>
// </service>
type Service struct {
	// Type indicates what kind of service this is (0-8, see above)
	// xml:"type" WITHOUT ,attr means: this is an XML element, not an attribute
	//
	// When Monit POSTs to /collector, the format is:
	// <service name="foo">
	//   <type>5</type>  ← type is an element
	// </service>
	//
	// Go's XML parser will handle both <type>5</type> and type="5" attribute
	Type int `xml:"type"`

	// Name is the service name (defined in monitrc)
	// xml:"name,attr" means: try the name attribute first
	// If not found, Go will also check for <name>...</name> element
	//
	// In collector format: <service name="foo">...</service>
	// In _status format: <service type="5"><name>foo</name>...</service>
	Name string `xml:"name,attr"`

	// CollectedSec is the Unix timestamp (seconds) when this data was collected
	CollectedSec int64 `xml:"collected_sec"`

	// CollectedUsec is the microseconds part of the timestamp
	// Full timestamp = CollectedSec + (CollectedUsec / 1000000)
	// Provides sub-second precision
	CollectedUsec int64 `xml:"collected_usec"`

	// Status indicates the current status of the service
	// 0 = OK/running
	// 1 = Failed/error
	// Other values = various states (see Monit documentation)
	Status int `xml:"status"`

	// StatusHint provides additional status information
	// 0 = no hint
	// 1 = hint available (check specific service type fields)
	StatusHint int `xml:"status_hint"`

	// Monitor indicates monitoring state
	// 0 = not monitored
	// 1 = monitored
	// 2 = initializing
	Monitor int `xml:"monitor"`

	// MonitorMode indicates the monitoring mode
	// 0 = active
	// 1 = passive
	// 2 = manual
	MonitorMode int `xml:"monitormode"`

	// OnReboot indicates what to do on system reboot
	// 0 = start (default)
	// 1 = nostart
	// 2 = laststate
	// 3 = ignore
	OnReboot int `xml:"onreboot"`

	// PendingAction indicates if an action is pending
	// 0 = no action pending
	// >0 = action number (restart, stop, etc.)
	PendingAction int `xml:"pendingaction"`

	// System contains system-level metrics (CPU, memory, load, swap)
	// Only present when Type == 5 (system service)
	// xml:",omitempty" means: if this is nil/empty, don't include it in output
	// (useful when generating XML, not needed for parsing, but good practice)
	System *SystemMetrics `xml:"system,omitempty"`

	// Process fields (for type 3 - process services)
	// These are directly in the <service> element, not nested
	// We use pointers (*int, *ProcessMemory) to distinguish between
	// "not present" (nil) and "present with value 0" (pointer to 0)
	PID      *int           `xml:"pid,omitempty"`
	PPID     *int           `xml:"ppid,omitempty"`
	UID      *int           `xml:"uid,omitempty"`
	EUID     *int           `xml:"euid,omitempty"`
	GID      *int           `xml:"gid,omitempty"`
	Uptime   *int64         `xml:"uptime,omitempty"`
	Boottime *int64         `xml:"boottime,omitempty"` // System boot time (Unix timestamp)
	Threads  *int           `xml:"threads,omitempty"`
	Children *int           `xml:"children,omitempty"`
	Memory   *ProcessMemory `xml:"memory,omitempty"`
	CPU      *ProcessCPU    `xml:"cpu,omitempty"`

	// Program contains program check information (exit status, output)
	// Only present when Type == 7 (program service)
	Program *ProgramInfo `xml:"program,omitempty"`

	// File contains file-specific information (size, permissions, checksum)
	// Only present when Type == 2 (file service)
	File *FileInfo `xml:",omitempty"`

	// Filesystem fields (for type 0 - filesystem services)
	// These are directly in the <service> element, not nested
	FSType   *string                   `xml:"fstype,omitempty"`
	FSFlags  *string                   `xml:"fsflags,omitempty"`
	FSMode   *string                   `xml:"mode,omitempty"`
	Block    *FilesystemBlock          `xml:"block,omitempty"`
	Inode    *FilesystemInode          `xml:"inode,omitempty"`
	ReadIO   *FilesystemIO             `xml:"read,omitempty"`
	WriteIO  *FilesystemIO             `xml:"write,omitempty"`

	// Network fields (for type 8 - network interface services)
	// Only present when Type == 8 (network interface)
	Link     *NetworkLink              `xml:"link,omitempty"`

	// Remote Host monitoring fields (for type 4 - remote host services)
	// ICMP contains ping monitoring information
	// Only present when Type == 4 (remote host) with ICMP checks
	ICMP *ICMPInfo `xml:"icmp,omitempty"`

	// Port contains TCP/UDP port monitoring information
	// Present when Type == 4 (remote host) with port checks
	// Also present when Type == 3 (process) with port checks
	Port *PortInfo `xml:"port,omitempty"`

	// Unix contains Unix domain socket monitoring information
	// Only present when Type == 3 (process) with unix socket checks
	Unix *UnixSocketInfo `xml:"unix,omitempty"`
}

// SystemMetrics contains system-level performance metrics.
//
// This is only present for system services (type 5).
//
// Example XML:
// <system>
//   <load><avg01>0.20</avg01><avg05>0.32</avg05><avg15>0.28</avg15></load>
//   <cpu><user>5.2</user><system>2.1</system><nice>0.0</nice></cpu>
//   <memory><percent>45.6</percent><kilobyte>12345678</kilobyte></memory>
//   <swap><percent>10.0</percent><kilobyte>1234567</kilobyte></swap>
// </system>
type SystemMetrics struct {
	// Load contains load average information
	// Load average is the average number of processes in the run queue
	// Values > number of CPUs indicate the system is busy/overloaded
	Load LoadAverage `xml:"load"`

	// CPU contains CPU usage percentages
	CPU CPUUsage `xml:"cpu"`

	// Memory contains RAM usage information
	Memory MemoryUsage `xml:"memory"`

	// Swap contains swap space usage information
	// Swap is disk space used when RAM is full
	// High swap usage often indicates insufficient RAM
	Swap SwapUsage `xml:"swap"`
}

// LoadAverage contains load average values.
//
// Load average is a Unix metric showing how busy the system is.
// It's the average number of processes waiting for CPU time.
//
// Three values represent different time windows:
// - 1 minute: very recent activity (responsive to spikes)
// - 5 minutes: recent trend
// - 15 minutes: longer-term trend
//
// Interpretation:
// - On a 4-core system:
//   - Load < 4.0: system not fully utilized
//   - Load = 4.0: all cores busy
//   - Load > 4.0: processes waiting for CPU time
type LoadAverage struct {
	// Avg01 is the 1-minute load average
	// float64 is a 64-bit floating point number (can have decimals)
	Avg01 float64 `xml:"avg01"`

	// Avg05 is the 5-minute load average
	Avg05 float64 `xml:"avg05"`

	// Avg15 is the 15-minute load average
	Avg15 float64 `xml:"avg15"`
}

// CPUUsage contains CPU usage percentages.
//
// These percentages add up to 100% (all CPU time):
// - User: time spent running user processes
// - System: time spent in kernel (OS operations)
// - Nice: time spent running "nice" (low priority) processes
// - Wait/IOWait: time waiting for I/O (disk, network)
// - Idle: time doing nothing
//
// Example: user=50, system=10, idle=40 means:
// - 50% of CPU time in user programs
// - 10% of CPU time in kernel
// - 40% of CPU time idle
type CPUUsage struct {
	// User is % of time in user mode (application code)
	User float64 `xml:"user"`

	// System is % of time in kernel mode (OS code)
	System float64 `xml:"system"`

	// Nice is % of time in low-priority processes
	// "nice" is a Unix command to run processes with lower priority
	Nice float64 `xml:"nice"`

	// HardIRQ is % of time handling hardware interrupts
	// (only on some systems, mainly Linux)
	HardIRQ float64 `xml:"hardirq"`

	// Wait is % of time waiting for I/O operations
	// High wait = bottleneck in disk or network
	Wait float64 `xml:"wait"`
}

// MemoryUsage contains RAM usage information.
type MemoryUsage struct {
	// Percent is % of RAM being used (0-100)
	Percent float64 `xml:"percent"`

	// Kilobyte is the amount of RAM used in kilobytes
	// To convert to GB: kilobyte / 1024 / 1024
	Kilobyte int64 `xml:"kilobyte"`
}

// SwapUsage contains swap space usage information.
//
// Swap is disk space used as "virtual memory" when RAM is full.
// Swap is MUCH slower than RAM (disk vs. memory speeds).
//
// Swap usage interpretation:
// - 0%: plenty of RAM available (good!)
// - 1-10%: RAM getting tight, occasional swapping
// - >10%: RAM insufficient, performance impact
// - >50%: serious RAM shortage, system very slow
type SwapUsage struct {
	// Percent is % of swap space being used (0-100)
	Percent float64 `xml:"percent"`

	// Kilobyte is the amount of swap used in kilobytes
	Kilobyte int64 `xml:"kilobyte"`
}

// ProcessInfo contains process-specific information.
//
// Only present for process services (type 3).
//
// Example XML:
// <pid>1234</pid>
// <ppid>1</ppid>
// <uid>0</uid>
// <memory><percent>2.5</percent><kilobyte>102400</kilobyte></memory>
// <cpu><percent>5.0</percent></cpu>
type ProcessInfo struct {
	// PID is the process ID
	// Every running process has a unique PID
	// PID 1 is always the init system (systemd, init, etc.)
	PID int `xml:"pid"`

	// PPID is the parent process ID
	// The process that started this process
	// If PPID is 1, this process was started by init
	PPID int `xml:"ppid"`

	// UID is the user ID that owns this process
	// 0 = root user
	// Other numbers = regular users
	UID int `xml:"uid"`

	// EUID is the effective user ID
	// Usually same as UID, but can differ for setuid programs
	EUID int `xml:"euid"`

	// GID is the group ID
	GID int `xml:"gid"`

	// Uptime is how long the process has been running (in seconds)
	Uptime int64 `xml:"uptime"`

	// Threads is the number of threads this process has
	// Modern programs often use multiple threads for concurrency
	Threads int `xml:"threads"`

	// Children is the number of child processes
	// For example, nginx master process has worker processes as children
	Children int `xml:"children"`

	// Memory is the memory usage of this process
	Memory ProcessMemory `xml:"memory"`

	// CPU is the CPU usage of this process
	CPU ProcessCPU `xml:"cpu"`
}

// ProcessMemory contains process memory usage.
type ProcessMemory struct {
	// Percent is this process's % of total system RAM
	Percent float64 `xml:"percent"`

	// PercentTotal is this process + children's % of total RAM
	PercentTotal float64 `xml:"percenttotal"`

	// Kilobyte is memory used by this process in KB
	Kilobyte int64 `xml:"kilobyte"`

	// KilobyteTotal is memory used by process + children in KB
	KilobyteTotal int64 `xml:"kilobytetotal"`
}

// ProcessCPU contains process CPU usage.
type ProcessCPU struct {
	// Percent is this process's % of total CPU time
	// 100% = using 1 full CPU core
	// 200% = using 2 full CPU cores
	Percent float64 `xml:"percent"`

	// PercentTotal is this process + children's % of total CPU
	PercentTotal float64 `xml:"percenttotal"`
}

// ProgramInfo contains program check information.
//
// Program checks run a command/script and check the exit status.
//
// Example XML:
// <program>
//   <started>1234567890</started>
//   <status>0</status>
//   <output><![CDATA[Everything OK]]></output>
// </program>
type ProgramInfo struct {
	// Started is Unix timestamp when the program was last run
	Started int64 `xml:"started"`

	// Status is the exit code from the program
	// 0 = success (standard Unix convention)
	// Non-zero = error/failure
	Status int `xml:"status"`

	// Output is the program's output (stdout)
	// <![CDATA[...]]> means "this is raw text, don't parse as XML"
	// Useful when output might contain <> characters
	Output string `xml:"output"`
}

// FileInfo contains file-specific information.
//
// Only present for file services (type 2).
//
// Example XML:
// <mode>644</mode>
// <uid>1000</uid>
// <gid>1000</gid>
// <size>12345</size>
// <hardlink>1</hardlink>
// <timestamps>
//   <access>1763943569</access>
//   <change>1763943568</change>
//   <modify>1763943568</modify>
// </timestamps>
// <checksum type="MD5">abc123...</checksum>
type FileInfo struct {
	// Mode is the Unix file permissions (octal format)
	// 644 = rw-r--r-- (owner can read/write, others can read)
	// 755 = rwxr-xr-x (owner can read/write/execute, others can read/execute)
	Mode string `xml:"mode"`

	// UID is the user ID that owns the file
	UID int `xml:"uid"`

	// GID is the group ID that owns the file
	GID int `xml:"gid"`

	// Size is the file size in bytes
	Size int64 `xml:"size"`

	// Hardlink is the number of hard links to the file
	Hardlink int `xml:"hardlink"`

	// Timestamps contains access, change, and modify times
	Timestamps FileTimestamps `xml:"timestamps"`

	// Checksum contains the file checksum and its type
	Checksum FileChecksum `xml:"checksum"`
}

// ICMPInfo contains ICMP (ping) monitoring information.
//
// Only present for Remote Host services (type 4) with ICMP checks.
//
// Example XML:
// <icmp>
//   <type>echo</type>
//   <responsetime>0.001</responsetime>
// </icmp>
type ICMPInfo struct {
	// Type is the ICMP check type (usually "echo" for ping)
	Type string `xml:"type"`

	// ResponseTime is the ping response time in seconds
	// Example: 0.001 = 1 millisecond
	ResponseTime float64 `xml:"responsetime"`
}

// PortInfo contains TCP/UDP port monitoring information.
//
// Only present for Remote Host services (type 4) with port checks,
// or Process services (type 3) with port checks.
//
// Example XML:
// <port>
//   <hostname>192.168.100.10</hostname>
//   <portnumber>8123</portnumber>
//   <request/>
//   <protocol>HTTP</protocol>
//   <type>TCP</type>
//   <responsetime>0.002</responsetime>
// </port>
type PortInfo struct {
	// Hostname is the target hostname or IP address
	Hostname string `xml:"hostname"`

	// PortNumber is the TCP/UDP port number
	PortNumber int `xml:"portnumber"`

	// Protocol is the application protocol (HTTP, HTTPS, etc.)
	Protocol string `xml:"protocol"`

	// Type is the transport protocol (TCP or UDP)
	Type string `xml:"type"`

	// ResponseTime is the port response time in seconds
	ResponseTime float64 `xml:"responsetime"`
}

// UnixSocketInfo contains Unix domain socket monitoring information.
//
// Only present for Process services (type 3) with unix socket checks.
//
// Example XML:
// <unix>
//   <path>/var/run/syslog.sock</path>
//   <protocol>DEFAULT</protocol>
//   <responsetime>0.000</responsetime>
// </unix>
type UnixSocketInfo struct {
	// Path is the filesystem path to the Unix socket
	Path string `xml:"path"`

	// Protocol is the socket protocol
	Protocol string `xml:"protocol"`

	// ResponseTime is the socket response time in seconds
	ResponseTime float64 `xml:"responsetime"`
}

// FilesystemInfo contains filesystem-specific information.
//
// Only present for filesystem services (type 0).
//
// Example XML:
// <fstype>zfs</fstype>
// <fsflags>nfs4acls, noatime, local</fsflags>
// <mode>755</mode>
// <uid>0</uid>
// <gid>0</gid>
// <block><percent>90.5</percent><usage>27226910.4</usage><total>30089135.8</total></block>
// <inode><percent>0.0</percent><usage>803919</usage><total>5862641503</total></inode>
// <read>...</read>
// <write>...</write>
type FilesystemInfo struct {
	// FSType is the filesystem type (zfs, ext4, xfs, etc.)
	FSType string `xml:"fstype"`

	// FSFlags contains mount flags (ro, rw, noatime, etc.)
	FSFlags string `xml:"fsflags"`

	// Mode is the Unix directory permissions (octal format)
	Mode string `xml:"mode"`

	// UID is the user ID that owns the filesystem mount point
	UID int `xml:"uid"`

	// GID is the group ID that owns the filesystem mount point
	GID int `xml:"gid"`

	// Block contains block/space usage information
	Block FilesystemBlock `xml:"block"`

	// Inode contains inode usage information
	Inode FilesystemInode `xml:"inode"`

	// Read contains read I/O statistics
	Read FilesystemIO `xml:"read"`

	// Write contains write I/O statistics
	Write FilesystemIO `xml:"write"`
}

// FilesystemBlock contains block/space usage information.
type FilesystemBlock struct {
	// Percent is the % of space used (0-100)
	Percent float64 `xml:"percent"`

	// Usage is the amount of space used (in MB)
	Usage float64 `xml:"usage"`

	// Total is the total space available (in MB)
	Total float64 `xml:"total"`
}

// FilesystemInode contains inode usage information.
//
// Inodes are filesystem data structures that store file metadata.
// Each file/directory requires one inode.
// Running out of inodes prevents creating new files even if space is available.
type FilesystemInode struct {
	// Percent is the % of inodes used (0-100)
	Percent float64 `xml:"percent"`

	// Usage is the number of inodes used
	Usage int64 `xml:"usage"`

	// Total is the total number of inodes available
	Total int64 `xml:"total"`
}

// FilesystemIO contains filesystem I/O statistics.
type FilesystemIO struct {
	// Bytes contains byte transfer statistics
	Bytes FilesystemBytes `xml:"bytes"`

	// Operations contains I/O operation count statistics
	Operations FilesystemOperations `xml:"operations"`
}

// FilesystemBytes contains byte transfer statistics.
type FilesystemBytes struct {
	// Count is the bytes transferred in the current interval
	Count int64 `xml:"count"`

	// Total is the total bytes transferred since system boot
	Total int64 `xml:"total"`
}

// FilesystemOperations contains I/O operation count statistics.
type FilesystemOperations struct {
	// Count is the number of operations in the current interval
	Count int64 `xml:"count"`

	// Total is the total number of operations since system boot
	Total int64 `xml:"total"`
}

// NetworkLink contains network interface link information.
//
// Only present for network interface services (type 8).
//
// Example XML:
// <link>
//   <state>1</state>
//   <speed>1000000000</speed>
//   <duplex>1</duplex>
//   <download>...</download>
//   <upload>...</upload>
// </link>
type NetworkLink struct {
	// State indicates link status
	// 1 = up (link active)
	// 0 = down (link inactive)
	State int `xml:"state"`

	// Speed is the link speed in bits per second
	// 1000000000 = 1 Gbps = 1000 Mb/s
	// 100000000 = 100 Mbps
	// 10000000 = 10 Mbps
	Speed int64 `xml:"speed"`

	// Duplex indicates duplex mode
	// 1 = full-duplex (can send and receive simultaneously)
	// 0 = half-duplex (can only send OR receive at a time)
	Duplex int `xml:"duplex"`

	// Download contains download/receive statistics
	Download NetworkTraffic `xml:"download"`

	// Upload contains upload/transmit statistics
	Upload NetworkTraffic `xml:"upload"`
}

// NetworkTraffic contains network traffic statistics (for download or upload).
type NetworkTraffic struct {
	// Packets contains packet statistics
	Packets NetworkCount `xml:"packets"`

	// Bytes contains byte transfer statistics
	Bytes NetworkCount `xml:"bytes"`

	// Errors contains error count statistics
	Errors NetworkCount `xml:"errors"`
}

// NetworkCount contains current and total counts for network metrics.
type NetworkCount struct {
	// Now is the current rate (per second for packets/bytes, current count for errors)
	Now int64 `xml:"now"`

	// Total is the cumulative total since system boot
	Total int64 `xml:"total"`
}

// FileTimestamps contains file timestamp information.
type FileTimestamps struct {
	// Access is the last access time (Unix timestamp)
	Access int64 `xml:"access"`

	// Change is the last change time (Unix timestamp)
	Change int64 `xml:"change"`

	// Modify is the last modification time (Unix timestamp)
	Modify int64 `xml:"modify"`
}

// FileChecksum contains checksum information for a monitored file.
type FileChecksum struct {
	// Type is the checksum algorithm (e.g., "MD5", "SHA1")
	Type string `xml:"type,attr"`

	// Value is the checksum hash value
	Value string `xml:",chardata"`
}

// StatusMessage returns a human-readable status message based on the status code.
// Monit uses numeric status codes that need to be translated for display.
//
// Status codes (from Monit documentation):
//   - 0 = Running/OK/Accessible
//   - 1 = Failed/Does not exist
//   - 2 = Resource limit matched (WARNING)
//   - 4 = Execution failed
//   - 8 = Not monitored
//   - 16 = Initializing
//
// Returns:
//   - string: Human-readable status message
func (s *Service) StatusMessage() string {
	switch s.Status {
	case 0:
		return "OK"
	case 1:
		return "Failed"
	case 2:
		return "Resource limit matched"
	case 4:
		return "Execution failed"
	case 8:
		return "Not monitored"
	case 16:
		return "Initializing"
	default:
		return fmt.Sprintf("Unknown status (%d)", s.Status)
	}
}
// ========================================================================
// XML Proxy Structs - Mirror Monit's flat XML structure
// ========================================================================

// ServiceXML is a proxy struct that mirrors Monit's flat XML structure.
// Monit sends fields like <uid>, <gid>, <mode> directly in <service>,
// but different service types (file, filesystem, process) use the same
// field names for different purposes. This proxy captures all flat fields,
// then ToService() converts them to the correct nested structure based on Type.
type ServiceXML struct {
	// Common fields (all service types)
	Type          int    `xml:"type"`          // Element: <type>5</type>
	Name          string `xml:"name,attr"`     // Attribute: <service name="bigone">
	CollectedSec  int64  `xml:"collected_sec"`
	CollectedUsec int64  `xml:"collected_usec"`
	Status        int    `xml:"status"`
	StatusHint    int    `xml:"status_hint"`
	Monitor       int    `xml:"monitor"`
	MonitorMode   int    `xml:"monitormode"`
	OnReboot      int    `xml:"onreboot"`
	PendingAction int    `xml:"pendingaction"`

	// Flat fields used by file (type 2), filesystem (type 0), and process (type 3)
	// These conflict - same XML tags used for different purposes
	Mode      *string `xml:"mode,omitempty"`       // File/filesystem mode
	UID       *int    `xml:"uid,omitempty"`        // File/filesystem/process UID
	GID       *int    `xml:"gid,omitempty"`        // File/filesystem/process GID
	Size      *int64  `xml:"size,omitempty"`       // File size
	Hardlink  *int    `xml:"hardlink,omitempty"`   // File hardlink count

	// File-specific nested fields (checksum and timestamps need special handling)
	Timestamps *FileTimestamps `xml:"timestamps,omitempty"` // File timestamps
	Checksum   *FileChecksum   `xml:"checksum,omitempty"`   // File checksum

	// Filesystem-specific fields
	FSType   *string           `xml:"fstype,omitempty"`  // Filesystem type
	FSFlags  *string           `xml:"fsflags,omitempty"` // Filesystem flags
	Block    *FilesystemBlock  `xml:"block,omitempty"`   // Filesystem blocks
	Inode    *FilesystemInode  `xml:"inode,omitempty"`   // Filesystem inodes
	ReadIO   *FilesystemIO     `xml:"read,omitempty"`    // Filesystem read IO
	WriteIO  *FilesystemIO     `xml:"write,omitempty"`   // Filesystem write IO

	// Process-specific fields
	PID      *int           `xml:"pid,omitempty"`
	PPID     *int           `xml:"ppid,omitempty"`
	EUID     *int           `xml:"euid,omitempty"`
	Uptime   *int64         `xml:"uptime,omitempty"`
	Boottime *int64         `xml:"boottime,omitempty"`
	Threads  *int           `xml:"threads,omitempty"`
	Children *int           `xml:"children,omitempty"`
	Memory   *ProcessMemory `xml:"memory,omitempty"`
	CPU      *ProcessCPU    `xml:"cpu,omitempty"`

	// Nested fields (these don't conflict)
	System  *SystemMetrics `xml:"system,omitempty"`
	Program *ProgramInfo   `xml:"program,omitempty"`
	Link    *NetworkLink   `xml:"link,omitempty"`

	// Remote host monitoring fields (for type 4 and type 3 with checks)
	ICMP    *ICMPInfo        `xml:"icmp,omitempty"`
	Port    *PortInfo        `xml:"port,omitempty"`
	Unix    *UnixSocketInfo  `xml:"unix,omitempty"`
}

// ToService converts the flat ServiceXML to the domain Service struct.
// It populates the correct nested structures based on the service Type.
func (sx *ServiceXML) ToService() Service {
	s := Service{
		Type:          sx.Type,
		Name:          sx.Name,
		CollectedSec:  sx.CollectedSec,
		CollectedUsec: sx.CollectedUsec,
		Status:        sx.Status,
		StatusHint:    sx.StatusHint,
		Monitor:       sx.Monitor,
		MonitorMode:   sx.MonitorMode,
		OnReboot:      sx.OnReboot,
		PendingAction: sx.PendingAction,
		System:        sx.System,
		Program:       sx.Program,
		Link:          sx.Link,
		ICMP:          sx.ICMP,
		Port:          sx.Port,
		Unix:          sx.Unix,
	}

	switch sx.Type {
	case 0: // Filesystem
		s.FSType = sx.FSType
		s.FSFlags = sx.FSFlags
		s.FSMode = sx.Mode // Mode goes to FSMode for filesystem
		s.Block = sx.Block
		s.Inode = sx.Inode
		s.ReadIO = sx.ReadIO
		s.WriteIO = sx.WriteIO
		// Note: filesystem also has uid/gid but they're not stored in domain model currently

	case 2: // File
		// Populate nested FileInfo struct from flat fields
		if sx.Mode != nil || sx.UID != nil || sx.GID != nil || sx.Size != nil {
			s.File = &FileInfo{
				Mode:       getStringValue(sx.Mode),
				UID:        getIntValue(sx.UID),
				GID:        getIntValue(sx.GID),
				Size:       getInt64Value(sx.Size),
				Hardlink:   getIntValue(sx.Hardlink),
				Timestamps: getFileTimestamps(sx.Timestamps),
				Checksum:   getFileChecksum(sx.Checksum),
			}
		}

	case 3: // Process
		s.PID = sx.PID
		s.PPID = sx.PPID
		s.UID = sx.UID
		s.EUID = sx.EUID
		s.GID = sx.GID
		s.Uptime = sx.Uptime
		s.Boottime = sx.Boottime
		s.Threads = sx.Threads
		s.Children = sx.Children
		s.Memory = sx.Memory
		s.CPU = sx.CPU
	}

	return s
}

// Helper functions to safely dereference pointers
func getStringValue(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func getIntValue(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}

func getInt64Value(p *int64) int64 {
	if p != nil {
		return *p
	}
	return 0
}

func getFileTimestamps(p *FileTimestamps) FileTimestamps {
	if p != nil {
		return *p
	}
	return FileTimestamps{}
}

func getFileChecksum(p *FileChecksum) FileChecksum {
	if p != nil {
		return *p
	}
	return FileChecksum{}
}

// MonitStatusXML is the XML proxy for the root monit element.
// It uses ServiceXML instead of Service to handle the flat XML structure.
//
// The Monit collector API sends XML with attributes on the <monit> element:
//   <monit id="..." incarnation="..." version="...">
// while the HTTP API (?format=xml) sends them as nested elements in <server>.
// We handle both formats by accepting them as optional attributes.
type MonitStatusXML struct {
	XMLName     xml.Name     `xml:"monit"`
	ID          string       `xml:"id,attr,omitempty"`          // Optional: collector format
	Incarnation string       `xml:"incarnation,attr,omitempty"` // Optional: collector format
	Version     string       `xml:"version,attr,omitempty"`     // Optional: collector format
	Server      Server       `xml:"server"`
	Platform    Platform     `xml:"platform"`
	ServicesWrapper struct {
		Services []ServiceXML `xml:"service"`
	} `xml:"services"`  // Monit sends services wrapped in <services> element
}

// ToMonitStatus converts MonitStatusXML to the domain MonitStatus struct.
func (msx *MonitStatusXML) ToMonitStatus() *MonitStatus {
	ms := &MonitStatus{
		Server:   msx.Server,
		Platform: msx.Platform,
		Services: make([]Service, len(msx.ServicesWrapper.Services)),
	}

	for i, svcXML := range msx.ServicesWrapper.Services {
		ms.Services[i] = svcXML.ToService()
	}

	return ms
}


// ParseMonitXML parses Monit XML status data into a MonitStatus struct.
//
// This function takes raw XML bytes (from an HTTP request body) and
// converts them into a usable Go data structure.
//
// Parameters:
//   - data: Raw XML bytes (from io.ReadAll(r.Body))
//
// Returns:
//   - *MonitStatus: Parsed status data (pointer to allow nil on error)
//   - error: nil if successful, error describing problem if failed
//
// How it works:
// 1. Create an empty MonitStatus struct
// 2. Use xml.Unmarshal() to parse the XML into the struct
// 3. The struct tags (like `xml:"monit"`) tell Unmarshal how to map XML to fields
// 4. Return the populated struct or an error
//
// Example usage:
//   xmlData := []byte("<monit>...</monit>")
//   status, err := ParseMonitXML(xmlData)
//   if err != nil {
//       log.Printf("Failed to parse XML: %v", err)
//       return
//   }
//   fmt.Printf("Host: %s\n", status.Server.LocalHostname)
func ParseMonitXML(data []byte) (*MonitStatus, error) {
	// Handle encoding declaration issue
	//
	// Monit sends XML with: <?xml version="1.0" encoding="ISO-8859-1"?>
	// Go's xml.Unmarshal() only handles UTF-8 natively.
	//
	// However, Monit's actual content is ASCII-compatible (doesn't use
	// extended ISO-8859-1 characters like é, ñ, etc. in practice).
	//
	// Solution: Replace "ISO-8859-1" with "UTF-8" in the XML declaration.
	// This works because:
	// 1. ASCII is a subset of both ISO-8859-1 and UTF-8
	// 2. Monit's data is actually ASCII (service names, hostnames, etc.)
	// 3. Even if there are extended characters, they'll parse correctly
	//
	// bytes.ReplaceAll() replaces all occurrences of a byte slice
	//   - Parameters: (data, old, new)
	//   - Returns: modified copy of data
	//
	// Note: This is safe because we're creating a copy, not modifying the original

	// DEBUG: Log first 500 bytes of XML before processing
	xmlPreview := string(data)
	if len(xmlPreview) > 500 {
		xmlPreview = xmlPreview[:500]
	}
	log.Printf("[DEBUG] Received XML (first 500 bytes): %s", xmlPreview)

	// DEBUG: Save full XML to file for analysis
	os.WriteFile("/tmp/cmonit-received-xml.xml", data, 0644)

	data = bytes.ReplaceAll(data, []byte("ISO-8859-1"), []byte("UTF-8"))

	// Alternative approach (if ReplaceAll doesn't work):
	// We could also just remove the encoding declaration entirely:
	// data = bytes.ReplaceAll(data, []byte(` encoding="ISO-8859-1"`), []byte(""))

	// PHASE 1: Unmarshal to proxy struct (MonitStatusXML)
	// This captures Monit's flat XML structure where fields like uid, gid, mode
	// appear directly in <service> elements for all service types.
	var statusXML MonitStatusXML

	err := xml.Unmarshal(data, &statusXML)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal XML: %w", err)
	}

	// DEBUG: Log how many services were parsed
	log.Printf("[DEBUG] Proxy unmarshal: parsed %d services from XML", len(statusXML.ServicesWrapper.Services))

	// PHASE 2: Convert proxy to domain model (MonitStatus)
	// ToMonitStatus() creates the proper nested structures (File, FileInfo, etc.)
	// based on service Type field, resolving field conflicts.
	status := statusXML.ToMonitStatus()

	log.Printf("[DEBUG] After ToMonitStatus conversion: %d services", len(status.Services))

	return status, nil
}

// GetCollectedTime converts the collected timestamp to a time.Time.
//
// Monit sends timestamps as two separate fields:
// - collected_sec: Unix timestamp (seconds since 1970)
// - collected_usec: Microseconds (0-999999)
//
// This helper function combines them into a single time.Time value.
//
// time.Time is Go's type for representing a moment in time.
// It includes date, time, timezone, and can be formatted/compared easily.
//
// Example usage:
//   service := status.Services[0]
//   timestamp := service.GetCollectedTime()
//   fmt.Println(timestamp.Format("2006-01-02 15:04:05"))
func (s *Service) GetCollectedTime() time.Time {
	// time.Unix() creates a time.Time from a Unix timestamp
	//
	// Parameters:
	//   - sec: seconds since Unix epoch (Jan 1, 1970 00:00:00 UTC)
	//   - nsec: nanoseconds (0-999999999)
	//
	// We convert microseconds to nanoseconds:
	//   - 1 second = 1,000,000 microseconds
	//   - 1 second = 1,000,000,000 nanoseconds
	//   - So: microseconds * 1000 = nanoseconds
	//
	// Returns: time.Time representing the exact moment the data was collected
	return time.Unix(s.CollectedSec, s.CollectedUsec*1000)
}
