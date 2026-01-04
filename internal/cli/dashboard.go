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

// Minimal color palette - orange accent, bright readable text
var (
	orange    = lipgloss.Color("#ff8800")
	white     = lipgloss.Color("#ffffff")
	lightGray = lipgloss.Color("#f0f0f0") // Very bright
	midGray   = lipgloss.Color("#cccccc") // Bright labels
	dimGray   = lipgloss.Color("#888888") // Still visible
	red       = lipgloss.Color("#ff6b6b")
)

// Styles
var (
	// Title style - uppercase, orange
	titleStyle = lipgloss.NewStyle().
			Foreground(orange).
			Bold(true).
			Padding(0, 1)

	// Section header - uppercase, bright
	headerStyle = lipgloss.NewStyle().
			Foreground(white).
			Bold(true)

	// Box with dim border (inactive)
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dimGray).
			Padding(1, 2)

	// Box with orange border (active)
	activeBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(orange).
			Padding(1, 2)

	// Label style - readable gray
	labelStyle = lipgloss.NewStyle().
			Foreground(midGray)

	// Value style - white for max readability
	valueStyle = lipgloss.NewStyle().
			Foreground(white)

	// Status styles
	statusOK = lipgloss.NewStyle().
			Foreground(orange)

	statusErr = lipgloss.NewStyle().
			Foreground(red)

	// Bracket style for [LABELS]
	bracketStyle = lipgloss.NewStyle().
			Foreground(midGray)

	bracketTextStyle = lipgloss.NewStyle().
				Foreground(lightGray)

	// Footer - readable
	footerStyle = lipgloss.NewStyle().
			Foreground(lightGray)

	keyStyle = lipgloss.NewStyle().
			Foreground(orange)
)

// Status data structures
type vmStatus struct {
	Running     bool
	Name        string
	IP          string
	CPUs        int
	MemoryMB    int
	MemoryUsed  int
	MemoryTotal int
	CPULoad     float64
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

type tickMsg time.Time
type statusUpdateMsg struct {
	linuxVM vmStatus
	microVM microVMStatus
	agent   agentStatus
	err     error
}
type actionResultMsg struct {
	action string
	err    error
}

func newDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Show live dashboard of VM and microVM status",
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
		func() tea.Msg { return m.fetchStatus() },
		tea.Every(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
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
			return m, func() tea.Msg { return m.fetchStatus() }
		case "s":
			if m.agent.FirecrackerRunning {
				return m, m.stopMicroVM
			}
		case "S":
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
			tea.Every(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
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
		return m, func() tea.Msg { return m.fetchStatus() }
	}

	return m, nil
}

func (m dashboardModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("FC-MACOS"))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("FIRECRACKER ON MACOS"))
	b.WriteString("\n\n")

	// Calculate widths
	availWidth := m.width
	if availWidth < 60 {
		availWidth = 80
	}

	boxWidth := (availWidth - 6) / 2
	if boxWidth > 40 {
		boxWidth = 40
	}

	// Layout
	if availWidth >= 85 {
		// Side by side - use same height for both boxes
		vmBox := m.renderVMBox(boxWidth, 10)
		agentBox := m.renderAgentBox(boxWidth, 10)
		row := lipgloss.JoinHorizontal(lipgloss.Top, vmBox, "  ", agentBox)
		b.WriteString(row)
		b.WriteString("\n\n")
		b.WriteString(m.renderMicroVMBox(boxWidth*2 + 4))
	} else {
		// Stacked
		b.WriteString(m.renderVMBox(availWidth-4, 0))
		b.WriteString("\n")
		b.WriteString(m.renderAgentBox(availWidth-4, 0))
		b.WriteString("\n")
		b.WriteString(m.renderMicroVMBox(availWidth - 4))
	}

	// Footer
	b.WriteString("\n\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m dashboardModel) renderVMBox(width int, height int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("LINUX VM"))
	lines = append(lines, "")

	if m.linuxVM.Running {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			statusOK.Render("✓"),
			valueStyle.Render("RUNNING")))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s  %s",
			labelStyle.Render("NAME"),
			valueStyle.Render(m.linuxVM.Name)))
		lines = append(lines, fmt.Sprintf("  %s    %s",
			labelStyle.Render("IP"),
			valueStyle.Render(m.linuxVM.IP)))
		lines = append(lines, "")

		// Resource meters
		if m.linuxVM.MemoryTotal > 0 {
			memPct := m.linuxVM.MemoryUsed * 100 / m.linuxVM.MemoryTotal
			lines = append(lines, m.renderMeter("MEM", memPct,
				fmt.Sprintf("%dM / %dM", m.linuxVM.MemoryUsed, m.linuxVM.MemoryTotal), width-8))
		}
		if m.linuxVM.CPUs > 0 {
			cpuPct := int(m.linuxVM.CPULoad / float64(m.linuxVM.CPUs) * 100)
			if cpuPct > 100 {
				cpuPct = 100
			}
			lines = append(lines, m.renderMeter("CPU", cpuPct,
				fmt.Sprintf("%.1f / %d", m.linuxVM.CPULoad, m.linuxVM.CPUs), width-8))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			statusErr.Render("✗"),
			labelStyle.Render("STOPPED")))
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("  run 'fc-macos setup' to start"))
	}

	content := strings.Join(lines, "\n")
	style := boxStyle
	if m.linuxVM.Running {
		style = activeBoxStyle
	}
	if height > 0 {
		style = style.Height(height)
	}
	return style.Width(width).Render(content)
}

func (m dashboardModel) renderAgentBox(width int, height int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("FC-AGENT"))
	lines = append(lines, "")

	if m.agent.Available {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			statusOK.Render("✓"),
			valueStyle.Render("ONLINE")))
		lines = append(lines, "")

		if m.agent.FirecrackerRunning {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				statusOK.Render("✓"),
				bracketTextStyle.Render("FIRECRACKER")))
			if m.agent.PID > 0 {
				lines = append(lines, fmt.Sprintf("  %s  %s",
					labelStyle.Render("PID"),
					valueStyle.Render(fmt.Sprintf("%d", m.agent.PID))))
			}
		} else {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				labelStyle.Render("○"),
				labelStyle.Render("FIRECRACKER")))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			statusErr.Render("✗"),
			labelStyle.Render("OFFLINE")))
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("  agent not responding"))
	}

	content := strings.Join(lines, "\n")
	style := boxStyle
	if m.agent.Available {
		style = activeBoxStyle
	}
	if height > 0 {
		style = style.Height(height)
	}
	return style.Width(width).Render(content)
}

func (m dashboardModel) renderMicroVMBox(width int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("MICROVM"))
	lines = append(lines, "")

	if !m.agent.FirecrackerRunning {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			labelStyle.Render("○"),
			labelStyle.Render("NOT RUNNING")))
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("  run 'fc-macos run' to start"))

		return boxStyle.Width(width).Render(strings.Join(lines, "\n"))
	}

	lines = append(lines, fmt.Sprintf("  %s  %s",
		statusOK.Render("✓"),
		valueStyle.Render("RUNNING")))
	lines = append(lines, "")

	// Resources in bracket style
	vcpuLabel := fmt.Sprintf("[ VCPUS: %d ]", m.microVM.VCPUs)
	memLabel := fmt.Sprintf("[ MEMORY: %d MiB ]", m.microVM.MemoryMiB)

	lines = append(lines, fmt.Sprintf("  %s  %s",
		bracketStyle.Render(vcpuLabel),
		bracketStyle.Render(memLabel)))
	lines = append(lines, "")

	// Resource bars
	barWidth := width - 16
	if barWidth < 20 {
		barWidth = 20
	}
	if barWidth > 50 {
		barWidth = 50
	}

	vcpuPct := m.microVM.VCPUs * 100 / 8
	memPct := m.microVM.MemoryMiB * 100 / 4096

	lines = append(lines, m.renderMeter("VCPU", vcpuPct, fmt.Sprintf("%d/8", m.microVM.VCPUs), barWidth))
	lines = append(lines, m.renderMeter("MEM", memPct, fmt.Sprintf("%d/4096", m.microVM.MemoryMiB), barWidth))

	return activeBoxStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (m dashboardModel) renderMeter(label string, pct int, suffix string, width int) string {
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}

	barWidth := width - len(label) - len(suffix) - 6
	if barWidth < 10 {
		barWidth = 10
	}

	filled := pct * barWidth / 100
	empty := barWidth - filled

	// Use block characters for a cleaner look
	filledBar := strings.Repeat("█", filled)
	emptyBar := strings.Repeat("░", empty)

	barStyle := lipgloss.NewStyle().Foreground(orange)
	emptyStyle := lipgloss.NewStyle().Foreground(dimGray)

	return fmt.Sprintf("  %s %s%s %s",
		labelStyle.Width(4).Render(label),
		barStyle.Render(filledBar),
		emptyStyle.Render(emptyBar),
		labelStyle.Render(suffix))
}

func (m dashboardModel) renderFooter() string {
	var parts []string

	// Timestamp
	if !m.lastUpdate.IsZero() {
		parts = append(parts, footerStyle.Render(m.lastUpdate.Format("15:04:05")))
	}

	// Error
	if m.err != nil {
		parts = append(parts, statusErr.Render(fmt.Sprintf("ERR: %v", m.err)))
	}

	// Commands
	var cmds []string
	cmds = append(cmds, fmt.Sprintf("%s refresh", keyStyle.Render("r")))
	if m.agent.FirecrackerRunning {
		cmds = append(cmds, fmt.Sprintf("%s stop microvm", keyStyle.Render("s")))
	}
	if m.linuxVM.Running {
		cmds = append(cmds, fmt.Sprintf("%s stop vm", keyStyle.Render("S")))
	}
	cmds = append(cmds, fmt.Sprintf("%s quit", keyStyle.Render("q")))

	parts = append(parts, footerStyle.Render(strings.Join(cmds, "  ")))

	return strings.Join(parts, "  │  ")
}

func (m dashboardModel) fetchStatus() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := statusUpdateMsg{}
	result.linuxVM = m.checkLinuxVM(ctx)

	if result.linuxVM.Running && result.linuxVM.IP != "" {
		result.agent = m.checkAgent(ctx, result.linuxVM.IP)
		if result.agent.Available && result.agent.FirecrackerRunning {
			result.microVM = m.checkMicroVM(ctx, result.linuxVM.IP)
		}
	}

	return result
}

func (m dashboardModel) checkLinuxVM(ctx context.Context) vmStatus {
	status := vmStatus{Name: m.vmName}

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
				fmt.Sscanf(fields[2], "%d", &status.CPUs)
				fmt.Sscanf(fields[3], "%d", &status.MemoryMB)
			}
			break
		}
	}

	if status.Running {
		ipCmd := exec.CommandContext(ctx, m.tartPath, "ip", m.vmName)
		if ipOut, err := ipCmd.Output(); err == nil {
			status.IP = strings.TrimSpace(string(ipOut))
		}

		memCmd := exec.CommandContext(ctx, m.tartPath, "exec", m.vmName, "sh", "-c",
			"free -m | awk '/^Mem:/ {print $2,$3}'")
		if memOut, err := memCmd.Output(); err == nil {
			fmt.Sscanf(strings.TrimSpace(string(memOut)), "%d %d", &status.MemoryTotal, &status.MemoryUsed)
		}

		loadCmd := exec.CommandContext(ctx, m.tartPath, "exec", m.vmName, "sh", "-c",
			"cat /proc/loadavg | awk '{print $1}'")
		if loadOut, err := loadCmd.Output(); err == nil {
			fmt.Sscanf(strings.TrimSpace(string(loadOut)), "%f", &status.CPULoad)
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
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:8080/agent/stop", m.linuxVM.IP), nil)
	resp, err := client.Do(req)
	if err != nil {
		return actionResultMsg{action: "stop-microvm", err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return actionResultMsg{action: "stop-microvm", err: fmt.Errorf("status %d", resp.StatusCode)}
	}
	return actionResultMsg{action: "stop-microvm"}
}

func (m dashboardModel) stopLinuxVM() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.tartPath, "stop", m.vmName)
	if err := cmd.Run(); err != nil {
		return actionResultMsg{action: "stop-vm", err: err}
	}
	return actionResultMsg{action: "stop-vm"}
}
