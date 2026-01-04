package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// Clean, readable color palette (brighter for dark terminals)
var (
	primaryColor   = lipgloss.Color("#ff8c42") // Orange for branding
	successColor   = lipgloss.Color("#2ecc71") // Green for running
	errorColor     = lipgloss.Color("#e74c3c") // Red for stopped
	mutedColor     = lipgloss.Color("#b0b0b0") // Light gray for labels
	textColor      = lipgloss.Color("#ffffff") // White for values
	dimColor       = lipgloss.Color("#888888") // Medium gray for borders/help
	accentColor    = lipgloss.Color("#3498db") // Blue accent
	barEmptyColor  = lipgloss.Color("#555555") // Medium dark for empty bar
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dimColor).
			Padding(0, 1)

	activeBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(successColor).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)

	labelStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	valueStyle = lipgloss.NewStyle().
			Foreground(textColor)

	runningStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	stoppedStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	keyStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)
)

// Status data structures
type vmStatus struct {
	Running bool
	Name    string
	IP      string
	CPUs    string
	Memory  string
}

type microVMStatus struct {
	Running   bool
	VCPUs     int
	MemoryMiB int
	PID       int
}

type agentStatus struct {
	Available          bool
	FirecrackerRunning bool
	PID                int
}

// Dashboard model
type dashboardModel struct {
	linuxVM    vmStatus
	microVM    microVMStatus
	agent      agentStatus
	lastUpdate time.Time
	err        error
	width      int
	height     int
	tartPath   string
	vmName     string
	quitting   bool
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

	m := dashboardModel{
		tartPath: tartPath,
		vmName:   "fc-macos-linux",
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.fetchStatus,
		tea.Every(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

// Action result messages
type actionResultMsg struct {
	action string
	err    error
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r":
			// Force refresh
			return m, func() tea.Msg {
				return m.fetchStatus()
			}
		case "s":
			// Stop microVM
			if m.agent.FirecrackerRunning {
				return m, m.stopMicroVM
			}
		case "S":
			// Stop Linux VM
			if m.linuxVM.Running {
				return m, m.stopLinuxVM
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			func() tea.Msg { return m.fetchStatus() },
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

	case actionResultMsg:
		m.err = msg.err
		// Refresh status after action
		return m, func() tea.Msg {
			return m.fetchStatus()
		}
	}

	return m, nil
}

func (m dashboardModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render("🔥 fc-macos")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Calculate available width
	availWidth := m.width
	if availWidth < 40 {
		availWidth = 80 // default
	}

	// Responsive layout
	if availWidth >= 80 {
		// Wide layout: boxes side by side
		boxWidth := (availWidth - 4) / 2
		if boxWidth > 38 {
			boxWidth = 38
		}

		vmBox := m.renderLinuxVMBox(boxWidth)
		agentBox := m.renderAgentBox(boxWidth)
		row := lipgloss.JoinHorizontal(lipgloss.Top, vmBox, " ", agentBox)
		b.WriteString(row)
		b.WriteString("\n")

		microVMBox := m.renderMicroVMBox(boxWidth*2 + 1)
		b.WriteString(microVMBox)
	} else {
		// Narrow layout: stack vertically
		boxWidth := availWidth - 2
		if boxWidth < 30 {
			boxWidth = 30
		}

		b.WriteString(m.renderLinuxVMBox(boxWidth))
		b.WriteString("\n")
		b.WriteString(m.renderAgentBox(boxWidth))
		b.WriteString("\n")
		b.WriteString(m.renderMicroVMBox(boxWidth))
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m dashboardModel) renderLinuxVMBox(width int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("Linux VM"))
	lines = append(lines, "")

	if m.linuxVM.Running {
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("Status:"),
			runningStyle.Render("● Running")))
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("Name:  "),
			valueStyle.Render(m.linuxVM.Name)))
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("IP:    "),
			valueStyle.Render(m.linuxVM.IP)))
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("CPUs:  "),
			valueStyle.Render(m.linuxVM.CPUs)))
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("Memory:"),
			valueStyle.Render(m.linuxVM.Memory)))
	} else {
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("Status:"),
			stoppedStyle.Render("● Stopped")))
		lines = append(lines, "")
		lines = append(lines, helpStyle.Render("Run 'fc-macos setup'"))
	}

	content := strings.Join(lines, "\n")
	style := boxStyle
	if m.linuxVM.Running {
		style = activeBoxStyle
	}
	return style.Width(width).Render(content)
}

func (m dashboardModel) renderAgentBox(width int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("fc-agent"))
	lines = append(lines, "")

	if m.agent.Available {
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("Status:"),
			runningStyle.Render("● Online")))

		if m.agent.FirecrackerRunning {
			lines = append(lines, fmt.Sprintf("%s %s",
				labelStyle.Render("Firecracker:"),
				runningStyle.Render("Running")))
			if m.agent.PID > 0 {
				lines = append(lines, fmt.Sprintf("%s %s",
					labelStyle.Render("PID:"),
					valueStyle.Render(fmt.Sprintf("%d", m.agent.PID))))
			}
		} else {
			lines = append(lines, fmt.Sprintf("%s %s",
				labelStyle.Render("Firecracker:"),
				stoppedStyle.Render("Stopped")))
		}
	} else {
		lines = append(lines, fmt.Sprintf("%s %s",
			labelStyle.Render("Status:"),
			stoppedStyle.Render("● Offline")))
		lines = append(lines, "")
		lines = append(lines, helpStyle.Render("Agent not responding"))
	}

	content := strings.Join(lines, "\n")
	style := boxStyle
	if m.agent.Available {
		style = activeBoxStyle
	}
	return style.Width(width).Render(content)
}

func (m dashboardModel) renderMicroVMBox(width int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("Firecracker MicroVM"))
	lines = append(lines, "")

	if !m.agent.FirecrackerRunning {
		lines = append(lines, stoppedStyle.Render("● Not Running"))
		lines = append(lines, "")
		lines = append(lines, helpStyle.Render("Run 'fc-macos run' to start"))

		return boxStyle.Width(width).Render(strings.Join(lines, "\n"))
	}

	lines = append(lines, runningStyle.Render("● Running"))
	lines = append(lines, "")

	// Resource bars - adjust bar width to fit
	barWidth := width - 20
	if barWidth < 15 {
		barWidth = 15
	}
	if barWidth > 40 {
		barWidth = 40
	}

	lines = append(lines, m.renderBar("vCPUs ", m.microVM.VCPUs, 8, barWidth))
	lines = append(lines, m.renderBar("Memory", m.microVM.MemoryMiB, 4096, barWidth))
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("%s %s    %s %s",
		labelStyle.Render("vCPUs:"),
		valueStyle.Render(fmt.Sprintf("%d", m.microVM.VCPUs)),
		labelStyle.Render("Memory:"),
		valueStyle.Render(fmt.Sprintf("%d MiB", m.microVM.MemoryMiB))))

	return activeBoxStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (m dashboardModel) renderBar(label string, used, max, barWidth int) string {
	percentage := float64(used) / float64(max)
	if percentage > 1 {
		percentage = 1
	}
	filled := int(percentage * float64(barWidth))

	// Simple two-color bar
	barFull := lipgloss.NewStyle().Foreground(accentColor).Render(strings.Repeat("█", filled))
	barEmpty := lipgloss.NewStyle().Foreground(barEmptyColor).Render(strings.Repeat("░", barWidth-filled))

	pct := int(percentage * 100)
	pctStr := fmt.Sprintf("%3d%%", pct)

	return fmt.Sprintf("%s %s%s %s",
		labelStyle.Width(7).Render(label),
		barFull, barEmpty,
		valueStyle.Render(pctStr))
}

func (m dashboardModel) renderFooter() string {
	var parts []string

	// Update time
	if !m.lastUpdate.IsZero() {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("Updated %s", m.lastUpdate.Format("15:04:05"))))
	}

	if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(errorColor)
		parts = append(parts, errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	// Help - show available actions
	var actions []string
	actions = append(actions, fmt.Sprintf("%s refresh", keyStyle.Render("r")))

	if m.agent.FirecrackerRunning {
		actions = append(actions, fmt.Sprintf("%s stop microVM", keyStyle.Render("s")))
	}
	if m.linuxVM.Running {
		actions = append(actions, fmt.Sprintf("%s stop VM", keyStyle.Render("S")))
	}
	actions = append(actions, fmt.Sprintf("%s quit", keyStyle.Render("q")))

	parts = append(parts, strings.Join(actions, "  "))

	return strings.Join(parts, "  │  ")
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
		// Only check microVM config if Firecracker is actually running
		// Otherwise querying /machine-config would auto-start Firecracker!
		if result.agent.Available && result.agent.FirecrackerRunning {
			result.microVM = m.checkMicroVM(ctx, result.linuxVM.IP)
		}
	}

	return result
}

func (m dashboardModel) checkLinuxVM(ctx context.Context) vmStatus {
	status := vmStatus{
		Name: m.vmName,
	}

	listCmd := exec.CommandContext(ctx, m.tartPath, "list")
	output, err := listCmd.Output()
	if err != nil {
		return status
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, m.vmName) {
			status.Running = strings.Contains(line, "running")
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				status.CPUs = fields[2]
				status.Memory = fields[3] + " MB"
			}
			break
		}
	}

	if status.Running {
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

	resp, err := client.Get(agentURL + "/health")
	if err != nil {
		return status
	}
	resp.Body.Close()
	status.Available = resp.StatusCode == 200

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

func (m dashboardModel) stopMicroVM() tea.Msg {
	if m.linuxVM.IP == "" {
		return actionResultMsg{action: "stop-microvm", err: fmt.Errorf("VM IP not available")}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	agentURL := fmt.Sprintf("http://%s:8080", m.linuxVM.IP)

	req, err := http.NewRequest("POST", agentURL+"/agent/stop", nil)
	if err != nil {
		return actionResultMsg{action: "stop-microvm", err: err}
	}

	resp, err := client.Do(req)
	if err != nil {
		return actionResultMsg{action: "stop-microvm", err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return actionResultMsg{action: "stop-microvm", err: fmt.Errorf("failed with status %d", resp.StatusCode)}
	}

	return actionResultMsg{action: "stop-microvm", err: nil}
}

func (m dashboardModel) stopLinuxVM() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.tartPath, "stop", m.vmName)
	if err := cmd.Run(); err != nil {
		return actionResultMsg{action: "stop-vm", err: err}
	}

	return actionResultMsg{action: "stop-vm", err: nil}
}
