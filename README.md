# **BBB: Bluesky Bot for Bacalhau**

## **Overview**
BBB is a bot that listens for mentions on [Bluesky](https://bsky.app/), detects specific commands, and executes jobs on the [Bacalhau](https://docs.bacalhau.org/) distributed compute network. It replies to users with execution details, including output and links to resources.

---

## **Features**
- **Command Detection:** Detects commands in mentions that follow the format: `@<username> job run <URL>`.
- **Job Dispatch:** Retrieves the job file from the provided URL, processes it, and submits it to Bacalhau.
- **Execution Monitoring:** Fetches execution details, including output, and replies to users with results.
- **Hyperlinked Replies:** Automatically includes clickable links in replies for documentation and resources.
- **Persistent Responses:** Keeps track of already responded mentions to avoid duplicate replies.

---

## **Requirements**
- **Go:** Version `1.22.2` or later.
- **Bacalhau Host:** The Bacalhau network hostname must be provided.
- **Bluesky Account:** The bot must be authenticated with valid credentials.

---

## **Setup**

### **1. Clone the Repository**
```bash
git clone https://github.com/seanmtracey/bacalhau-bluesky-bot
cd bacalhau-bluesky-bot
```

### **2. Install Dependencies**
```bash
go mod tidy
```

### **3. Environment Variables**
Create a .env file in the root directory and add the following:

```bash
BLUESKY_USER=your-bluesky-username
BLUESKY_PASS=your-bluesky-password
BACALHAU_HOST=bootstrap.production.bacalhau.org:1234
```

### **4. Build the Binary**
```bash
go build -o bbb
```
### **5. Run the Bot**
```bash
./bbb
```
