# cmonit Documentation

## Quick Links

- **[Project Plan](project-plan.md)** - Complete project roadmap, phases, and architecture
- **[Monit Collector Protocol](monit-collector-protocol.md)** - Technical details of Monit→M/Monit communication
- **[Monit XML Reference](monit-xml-reference.md)** - Complete reference of all Monit XML fields (used and unused)
- **[Testing Plan](testing-plan.md)** - Detailed acceptance tests for each phase
- **[Coding Standards](coding-standards.md)** - Educational commenting guidelines (MUST READ!)

---

## Project Overview

**cmonit** (Central Monit) is an open-source clone of the proprietary M/Monit software. It provides centralized monitoring and management of all Monit-enabled hosts through a modern, clean, mobile-friendly web interface.

### Key Features

✅ **HTTP Collector** - Receives status updates from Monit agents
✅ **SQLite Database** - Stores current status and historical metrics
✅ **Web Dashboard** - Real-time status view of all monitored hosts/services
✅ **Time-Series Graphs** - Visualize CPU, memory, disk, network metrics over time
✅ **M/Monit API Compatible** - REST API compatible with M/Monit tools
✅ **Single Binary** - No dependencies, easy deployment
✅ **Mobile Friendly** - Responsive design works on phones/tablets

---

## Technology Stack - Approved ✅

Your proposed technology stack is **excellent** and follows KISS principles:

### Backend: **Go (Golang)** ✅

**Why Go is the right choice:**
- ✅ Single binary deployment (just `./cmonit`)
- ✅ Excellent HTTP/concurrent request handling
- ✅ Built-in templating for HTML
- ✅ Cross-platform (FreeBSD, Linux, macOS)
- ✅ Low memory footprint
- ✅ Easy to maintain
- ✅ Standard library handles XML, HTTP, SQLite

**Alternatives considered:**
- Python: ❌ Requires runtime, slower for HTTP services
- Node.js: ❌ Requires runtime, npm dependency complexity
- Rust: ❌ Steeper learning curve, overkill for this project

**Verdict:** Go is perfect for this use case.

### Database: **SQLite** ✅

**Why SQLite is the right choice:**
- ✅ No separate database server needed
- ✅ Zero configuration
- ✅ File-based (easy backup: just copy the .db file)
- ✅ Perfect for embedded systems
- ✅ Handles 100+ hosts easily
- ✅ ACID compliant

**Alternatives considered:**
- PostgreSQL/MySQL: ❌ Overkill, requires separate server
- InfluxDB: ❌ Adds complexity, unnecessary for this scale

**Verdict:** SQLite is ideal for small-to-medium deployments. Can migrate to PostgreSQL later if needed (1000+ hosts).

### Frontend: **Tailwind CSS + Chart.js** ✅

**Why this combination is perfect:**
- ✅ No build step required (CDN-based)
- ✅ Modern, responsive design out of the box
- ✅ Mobile-friendly by default
- ✅ Chart.js is simple and lightweight
- ✅ Can use Go's `html/template` for server-side rendering

**Verdict:** Perfect choice for a clean, simple, maintainable UI.

---

## Answers to Your Questions

### Q: What do you think of the current proposal?

**A:** The proposal is **excellent**! The technology choices are sound, and the phased approach is correct:

1. ✅ Build collector daemon first (foundation)
2. ✅ Store data in SQLite
3. ✅ Create minimal web UI with tables
4. ✅ Add time-series graphs

This follows proper software engineering: **data layer → API layer → presentation layer**.

### Q: Do you need multiple Monit clients for testing?

**A:** It depends on the phase:

**Phase 1 & 2 (Collector + Dashboard):**
- ✅ **Single Monit client is sufficient**
- You already have one running with multiple services
- This is enough to validate basic functionality

**Phase 3 (Graphs):**
- ✅ **Single Monit client is still fine**
- Time-series graphs just need data over time
- Let collector run for 1+ hours to accumulate data

**Phase 4+ (Load Testing):**
- ⚠️ **Multiple clients recommended**
- Test with 10-50 simulated hosts
- Options:
  1. **Docker containers** - Easiest (if Docker available)
  2. **FreeBSD jails** - Native FreeBSD solution
  3. **Mock data generator** - Write Go tool to simulate multiple hosts

**Recommendation:** Start with your existing single Monit agent. Add multiple test clients only when needed for performance testing (Phase 4+).

### Q: What about tests for each step?

**A:** Yes! The [Testing Plan](testing-plan.md) includes **detailed acceptance tests** for each phase:

- **Phase 1:** 16 tests (T1.1 - T1.16)
- **Phase 2:** 10 tests (T2.1 - T2.10)
- **Phase 3:** 15 tests (T3.1 - T3.15)

Each test includes:
- Clear description
- Step-by-step instructions
- Exact commands to run
- Expected results
- Pass/fail criteria

**Testing approach:**
```
Plan → Act → Validate
  ↑              ↓
  └──────────────┘
```

You must NOT proceed to the next step until the current test passes!

---

## Development Roadmap

### Phase 1: Collector Daemon (Days 1-2)

**Goal:** Receive and store Monit status data

**Components:**
- HTTP server on port 8080
- `/collector` endpoint (POST)
- HTTP Basic Auth
- XML parser
- SQLite database
- Data insertion

**Tests:** 16 acceptance tests (T1.1 - T1.16)

**Deliverable:** Working daemon collecting data from your running Monit agent

---

### Phase 2: Web Dashboard (Days 3-4)

**Goal:** Display current status of all hosts/services

**Components:**
- Web server on port 3000
- Dashboard page (HTML template)
- Service status table
- Tailwind CSS styling
- Auto-refresh (30s)

**Tests:** 10 acceptance tests (T2.1 - T2.10)

**Deliverable:** Functional web dashboard showing real-time status

---

### Phase 3: Time-Series Graphs (Days 5-6)

**Goal:** Visualize metrics over time

**Components:**
- Graphs page per host
- Chart.js integration
- Time-series queries
- Time range selector (1h, 6h, 24h, 7d, 30d)

**Tests:** 15 acceptance tests (T3.1 - T3.15)

**Deliverable:** Interactive graphs for CPU, memory, disk, load average

---

### Phase 4: M/Monit API (Days 7-8)

**Goal:** API compatibility with M/Monit

**Components:**
- REST API endpoints
- JSON responses
- Authentication
- Documentation

**Tests:** API integration tests (TBD)

**Deliverable:** M/Monit-compatible REST API

---

## Getting Started

### Prerequisites

- Go 1.21+ installed
- FreeBSD system (your current environment)
- Running Monit agent (already configured)

### Project Structure

```
cmonit/
├── cmd/
│   └── cmonit/
│       └── main.go              # Entry point
├── internal/
│   ├── api/
│   │   ├── collector.go         # /collector endpoint
│   │   └── handlers.go
│   ├── db/
│   │   ├── schema.go            # Database schema
│   │   ├── models.go            # Data models
│   │   └── queries.go
│   ├── parser/
│   │   └── xml.go               # XML parser
│   ├── web/
│   │   ├── server.go
│   │   └── handlers.go
│   └── config/
│       └── config.go
├── web/
│   ├── templates/
│   │   ├── base.html
│   │   ├── dashboard.html
│   │   ├── host.html
│   │   └── graphs.html
│   └── static/
├── docs/                        # This directory!
│   ├── README.md
│   ├── project-plan.md
│   ├── monit-collector-protocol.md
│   └── testing-plan.md
├── LICENSE
├── go.mod
├── go.sum
├── Makefile
└── .gitignore
```

### First Steps

Ready to start coding? Here's what to do:

1. **Initialize Go module:**
   ```bash
   cd /usr/home/olivier/cmonit
   go mod init github.com/ocochard/cmonit
   ```

2. **Create project structure:**
   ```bash
   mkdir -p cmd/cmonit
   mkdir -p internal/{api,db,parser,web,config}
   mkdir -p web/{templates,static}
   ```

3. **Start with Phase 1, Test T1.1:**
   - Create `cmd/cmonit/main.go`
   - Basic HTTP server listening on :8080
   - Run test T1.1 (Server Startup)

4. **Follow the plan:**
   - One test at a time
   - Plan → Act → Validate
   - Only proceed when test passes

---

## Current System Status

Your FreeBSD system has:
- ✅ Monit running (`/usr/local/bin/monit`)
- ✅ Configuration: `/usr/local/etc/monitrc`
- ✅ Collector URL configured: `http://monit:monit@127.0.0.1:8080/collector`
  - ⚠️ **Note:** There's a typo in the config: `127.0.0:1` should be `127.0.0.1`
  - Fix this before testing!
- ✅ Monitored services:
  - System (CPU, memory, swap, load)
  - sshd process
  - nginx process
  - SSL certificate file
  - Temperature check script

### Fix the Monit Configuration Typo

Before starting development:

```bash
sudo vi /usr/local/etc/monitrc

# Change this line:
# set mmonit http://monit:monit@127.0.0:1:8080/collector

# To:
set mmonit http://monit:monit@127.0.0.1:8080/collector

# Reload monit:
sudo monit reload
```

---

## Expected Workflow

### Day 1-2: Phase 1 (Collector)

```bash
# Morning: Setup + T1.1-T1.6
git checkout -b phase1-collector
# Write basic HTTP server
# Implement authentication
# Create database schema
# Test T1.1-T1.6

# Afternoon: T1.7-T1.16
# Implement XML parser
# Connect to database
# Store parsed data
# Test T1.7-T1.16

# End of day:
# ✅ All Phase 1 tests pass
# ✅ Real Monit data flowing into SQLite
git commit -m "Phase 1: Collector daemon complete"
```

### Day 3-4: Phase 2 (Dashboard)

```bash
git checkout -b phase2-dashboard
# Create web server
# Build HTML templates
# Query database for display
# Add Tailwind CSS
# Implement auto-refresh
# Test T2.1-T2.10

# End of day:
# ✅ All Phase 2 tests pass
# ✅ Web dashboard showing live data
git commit -m "Phase 2: Web dashboard complete"
```

### Day 5-6: Phase 3 (Graphs)

```bash
git checkout -b phase3-graphs
# Create graph templates
# Implement time-series queries
# Integrate Chart.js
# Add time range selectors
# Test T3.1-T3.15

# End of day:
# ✅ All Phase 3 tests pass
# ✅ Beautiful time-series graphs
git commit -m "Phase 3: Time-series graphs complete"
```

---

## Success Criteria

The project is successful when:

1. ✅ cmonit receives data from your Monit agent
2. ✅ SQLite database stores current status + historical metrics
3. ✅ Web dashboard displays all monitored hosts/services
4. ✅ Time-series graphs show CPU, memory, disk, load metrics
5. ✅ All acceptance tests pass
6. ✅ System runs stably for 24+ hours
7. ✅ Single binary deployment works

---

## Performance Targets

- **API Response:** < 100ms
- **Dashboard Load:** < 500ms
- **Graph Render:** < 2s (24h of data)
- **Memory Usage:** < 50 MB
- **Database Size:** ~1 MB per day per host

---

## Documentation Quality

All documentation is:
- ✅ Clear and concise
- ✅ Includes examples
- ✅ Step-by-step instructions
- ✅ Covers all phases
- ✅ Includes testing strategy
- ✅ Addresses your specific questions

---

## Next Steps

Ready to start? Here's your immediate action plan:

1. ✅ **Read all documentation** (you're doing this now!)
2. ⬜ **Fix Monit configuration typo** (127.0.0:1 → 127.0.0.1)
3. ⬜ **Initialize Go project** (`go mod init`)
4. ⬜ **Create directory structure**
5. ⬜ **Start Phase 1, Test T1.1** (Basic HTTP server)
6. ⬜ **Follow Plan → Act → Validate loop**
7. ⬜ **Do NOT proceed until each test passes**

---

## Questions or Issues?

If you have questions or need clarification:
1. Check the relevant documentation file
2. Review the Monit source code (`/usr/home/olivier/monit-5.35.2/`)
3. Test with your running Monit agent
4. Ask for help if stuck!

---

## License

BSD 2-Clause License (see LICENSE file)

---

**Let's build this! 🚀**

The plan is solid, the technology stack is perfect, and the testing strategy is comprehensive. Follow the phases one test at a time, and you'll have a working cmonit system in about a week.

Good luck! 🎯
