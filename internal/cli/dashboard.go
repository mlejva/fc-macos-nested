package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2)

	activeBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("40")).
			Padding(1, 2)

	inactiveBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(1, 2)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Bold(true)

	runningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("40")).
			Bold(true)

	stoppedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	barEmptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	barFullStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("40"))

	barHighStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	barCriticalStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))
)

// Status data structures
type vmStatus struct {
	Running   bool
	Name      string
	IP        string
	CPUs      string
	Memory    string
	DiskUsage string
}

type microVMStatus struct {
	Running    bool
	VCPUs      int
	MemoryMiB  int
	PID        int
	SocketPath string
}

type agentStatus struct {
	Available         bool
	FirecrackerRunning bool
	PID               int
}

// Dashboard model
type dashboardModel struct {
	spinner      spinner.Model
	linuxVM      vmStatus
	microVM      microVMStatus
	agent        agentStatus
	lastUpdate   time.Time
	err          error
	width        int
	height       int
	tartPath     string
	vmName       string
	quitting     bool
}

// Messages
type tickMsg time.Time
type statusUpdateMsg struct {
	linuxVM vmStatus
	microVM microVMStatus
	agent   agentStatus
	err     error
}

func newDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Show live dashboard of VM and microVM status",
		Long: `Display a live dashboard showing:
- Linux VM status (running, IP, resources)
- MicroVM status (running, vCPUs, memory)
- Resource consumption with visual bars
- Auto-refreshes every 2 seconds`,
		Example: `  # Show live dashboard
  fc-macos dashboard`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDashboard(cmd.Context())
		},
	}
	return cmd
}

func runDashboard(ctx context.Context) error {
	tartPath := findTart()
	if tartPath == "" {
		return fmt.Errorf("tart not found")
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	m := dashboardModel{
		spinner:  s,
		tartPath: tartPath,
		vmName:   "fc-macos-linux",
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchStatus,
		tea.Every(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, m.fetchStatus
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			m.fetchStatus,
			tea.Every(2*time.Second, func(t time.Time) tea.Msg {
				return tickMsg(t)
			}),
		)

	case statusUpdateMsg:
		m.linuxVM = msg.linuxVM
		m.microVM = msg.microVM
		m.agent = msg.agent
		m.err = msg.err
		m.lastUpdate = time.Now()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m dashboardModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render("🔥 fc-macos Dashboard")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Linux VM Box
	vmBox := m.renderLinuxVMBox()

	// Agent Box
	agentBox := m.renderAgentBox()

	// MicroVM Box
	microVMBox := m.renderMicroVMBox()

	// Layout boxes horizontally if enough width
	if m.width > 100 {
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, vmBox, "  ", agentBox)
		b.WriteString(row1)
		b.WriteString("\n\n")
		b.WriteString(microVMBox)
	} else {
		b.WriteString(vmBox)
		b.WriteString("\n\n")
		b.WriteString(agentBox)
		b.WriteString("\n\n")
		b.WriteString(microVMBox)
	}

	// Footer
	b.WriteString("\n")
	updateInfo := fmt.Sprintf("Last update: %s", m.lastUpdate.Format("15:04:05"))
	if m.err != nil {
		updateInfo += " " + warningStyle.Render(fmt.Sprintf("(error: %v)", m.err))
	}
	b.WriteString(helpStyle.Render(updateInfo))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Press 'r' to refresh • 'q' to quit"))

	return b.String()
}

func (m dashboardModel) renderLinuxVMBox() string {
	var content strings.Builder

	content.WriteString(headerStyle.Render("🐧 Linux VM"))
	content.WriteString("\n\n")

	// Status
	status := stoppedStyle.Render("● Stopped")
	if m.linuxVM.Running {
		status = runningStyle.Render("● Running")
	}
	content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Status:"), status))

	// Details
	if m.linuxVM.Running {
		content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Name:  "), valueStyle.Render(m.linuxVM.Name)))
		content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("IP:    "), valueStyle.Render(m.linuxVM.IP)))
		content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("CPUs:  "), valueStyle.Render(m.linuxVM.CPUs)))
		content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Memory:"), valueStyle.Render(m.linuxVM.Memory)))
	} else {
		content.WriteString(labelStyle.Render("\nRun 'fc-macos setup' to start"))
	}

	style := inactiveBoxStyle
	if m.linuxVM.Running {
		style = activeBoxStyle
	}
	return style.Width(35).Render(content.String())
}

func (m dashboardModel) renderAgentBox() string {
	var content strings.Builder

	content.WriteString(headerStyle.Render("🤖 fc-agent"))
	content.WriteString("\n\n")

	// Status
	status := stoppedStyle.Render("● Offline")
	if m.agent.Available {
		status = runningStyle.Render("● Online")
	}
	content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Status:"), status))

	if m.agent.Available {
		fcStatus := stoppedStyle.Render("Stopped")
		if m.agent.FirecrackerRunning {
			fcStatus = runningStyle.Render("Running")
		}
		content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Firecracker:"), fcStatus))
		if m.agent.PID > 0 {
			content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("FC PID:"), valueStyle.Render(fmt.Sprintf("%d", m.agent.PID))))
		}
	} else {
		content.WriteString(labelStyle.Render("\nAgent not responding"))
	}

	style := inactiveBoxStyle
	if m.agent.Available {
		style = activeBoxStyle
	}
	return style.Width(35).Render(content.String())
}

func (m dashboardModel) renderMicroVMBox() string {
	var content strings.Builder

	content.WriteString(headerStyle.Render("🔥 Firecracker MicroVM"))
	content.WriteString("\n\n")

	if !m.agent.FirecrackerRunning {
		content.WriteString(stoppedStyle.Render("● Not Running"))
		content.WriteString("\n\n")
		content.WriteString(labelStyle.Render("Run 'fc-macos run' to start a microVM"))
		return inactiveBoxStyle.Width(74).Render(content.String())
	}

	content.WriteString(runningStyle.Render("● Running"))
	content.WriteString("\n\n")

	// Resource bars
	content.WriteString(m.renderResourceBar("vCPUs", m.microVM.VCPUs, 8))
	content.WriteString("\n")
	content.WriteString(m.renderMemoryBar("Memory", m.microVM.MemoryMiB, 4096))
	content.WriteString("\n\n")

	// Details
	content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("vCPUs: "), valueStyle.Render(fmt.Sprintf("%d", m.microVM.VCPUs))))
	content.WriteString(fmt.Sprintf("%s %s\n", labelStyle.Render("Memory:"), valueStyle.Render(fmt.Sprintf("%d MiB", m.microVM.MemoryMiB))))

	return activeBoxStyle.Width(74).Render(content.String())
}

func (m dashboardModel) renderResourceBar(label string, used, max int) string {
	barWidth := 40
	percentage := float64(used) / float64(max)
	if percentage > 1 {
		percentage = 1
	}
	filled := int(percentage * float64(barWidth))

	var barStyle lipgloss.Style
	if percentage < 0.6 {
		barStyle = barFullStyle
	} else if percentage < 0.85 {
		barStyle = barHighStyle
	} else {
		barStyle = barCriticalStyle
	}

	bar := barStyle.Render(strings.Repeat("█", filled)) +
		barEmptyStyle.Render(strings.Repeat("░", barWidth-filled))

	return fmt.Sprintf("%s [%s] %d/%d",
		labelStyle.Width(8).Render(label),
		bar,
		used, max)
}

func (m dashboardModel) renderMemoryBar(label string, usedMiB, maxMiB int) string {
	barWidth := 40
	percentage := float64(usedMiB) / float64(maxMiB)
	if percentage > 1 {
		percentage = 1
	}
	filled := int(percentage * float64(barWidth))

	var barStyle lipgloss.Style
	if percentage < 0.6 {
		barStyle = barFullStyle
	} else if percentage < 0.85 {
		barStyle = barHighStyle
	} else {
		barStyle = barCriticalStyle
	}

	bar := barStyle.Render(strings.Repeat("█", filled)) +
		barEmptyStyle.Render(strings.Repeat("░", barWidth-filled))

	return fmt.Sprintf("%s [%s] %d/%d MiB",
		labelStyle.Width(8).Render(label),
		bar,
		usedMiB, maxMiB)
}

func (m dashboardModel) fetchStatus() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := statusUpdateMsg{}

	// Check Linux VM status
	result.linuxVM = m.checkLinuxVM(ctx)

	// If VM is running, check agent and microVM
	if result.linuxVM.Running && result.linuxVM.IP != "" {
		result.agent = m.checkAgent(ctx, result.linuxVM.IP)
		if result.agent.Available {
			result.microVM = m.checkMicroVM(ctx, result.linuxVM.IP)
		}
	}

	return result
}

func (m dashboardModel) checkLinuxVM(ctx context.Context) vmStatus {
	status := vmStatus{
		Name: m.vmName,
	}

	// Check if VM is running
	listCmd := exec.CommandContext(ctx, m.tartPath, "list")
	output, err := listCmd.Output()
	if err != nil {
		return status
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, m.vmName) {
			status.Running = strings.Contains(line, "running")
			// Parse CPU and memory from tart list output
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				status.CPUs = fields[2]
				status.Memory = fields[3] + " MB"
			}
			break
		}
	}

	if status.Running {
		// Get IP
		ipCmd := exec.CommandContext(ctx, m.tartPath, "ip", m.vmName)
		if ipOut, err := ipCmd.Output(); err == nil {
			status.IP = strings.TrimSpace(string(ipOut))
		}
	}

	return status
}

func (m dashboardModel) checkAgent(ctx context.Context, vmIP string) agentStatus {
	status := agentStatus{}

	client := &http.Client{Timeout: 2 * time.Second}
	agentURL := fmt.Sprintf("http://%s:8080", vmIP)

	// Health check
	resp, err := client.Get(agentURL + "/health")
	if err != nil {
		return status
	}
	resp.Body.Close()
	status.Available = resp.StatusCode == 200

	// Get detailed status
	resp, err = client.Get(agentURL + "/agent/status")
	if err != nil {
		return status
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return status
	}

	if running, ok := data["firecracker_running"].(bool); ok {
		status.FirecrackerRunning = running
	}
	if pid, ok := data["pid"].(float64); ok {
		status.PID = int(pid)
	}

	return status
}

func (m dashboardModel) checkMicroVM(ctx context.Context, vmIP string) microVMStatus {
	status := microVMStatus{}

	client := &http.Client{Timeout: 2 * time.Second}
	agentURL := fmt.Sprintf("http://%s:8080", vmIP)

	// Get machine config from Firecracker
	resp, err := client.Get(agentURL + "/machine-config")
	if err != nil {
		return status
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return status
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return status
	}

	status.Running = true
	if vcpus, ok := data["vcpu_count"].(float64); ok {
		status.VCPUs = int(vcpus)
	}
	if mem, ok := data["mem_size_mib"].(float64); ok {
		status.MemoryMiB = int(mem)
	}

	return status
}
