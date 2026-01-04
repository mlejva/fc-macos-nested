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
	ID          string
	Name        string
	Running     bool
	VCPUs       int
	MemoryMiB   int
	PID         int
	CPUPercent  float64 // CPU usage percentage
	MemoryUsedM int     // Memory used in MiB
}

type agentStatus struct {
	Available          bool
	FirecrackerRunning bool
	TotalVMs           int
	RunningVMs         int
}

type dashboardModel struct {
	linuxVM     vmStatus
	microVMs    []microVMStatus
	agent       agentStatus
	lastUpdate  time.Time
	err         error
	width       int
	height      int
	tartPath    string
	vmName      string
	quitting    bool
	selectedIdx int             // Currently selected microVM
	listOffset  int             // Scroll offset for long lists
	maxVisible  int             // Max visible microVMs (5)
	expandedVMs map[string]bool // Track which VMs have details expanded
}

type tickMsg time.Time
type statusUpdateMsg struct {
	linuxVM  vmStatus
	microVMs []microVMStatus
	agent    agentStatus
	err      error
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
		tartPath:    tartPath,
		vmName:      "fc-macos-linux",
		maxVisible:  5,
		expandedVMs: make(map[string]bool),
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
		case "j", "down":
			// Move selection down
			if len(m.microVMs) > 0 && m.selectedIdx < len(m.microVMs)-1 {
				m.selectedIdx++
				// Adjust scroll offset if needed
				if m.selectedIdx >= m.listOffset+m.maxVisible {
					m.listOffset = m.selectedIdx - m.maxVisible + 1
				}
			}
			return m, nil
		case "k", "up":
			// Move selection up
			if m.selectedIdx > 0 {
				m.selectedIdx--
				// Adjust scroll offset if needed
				if m.selectedIdx < m.listOffset {
					m.listOffset = m.selectedIdx
				}
			}
			return m, nil
		case "enter", " ":
			// Toggle details for selected microVM
			if len(m.microVMs) > 0 && m.selectedIdx < len(m.microVMs) {
				vmID := m.microVMs[m.selectedIdx].ID
				m.expandedVMs[vmID] = !m.expandedVMs[vmID]
			}
			return m, nil
		case "s":
			// Stop selected microVM
			if len(m.microVMs) > 0 && m.selectedIdx < len(m.microVMs) {
				return m, m.stopSelectedMicroVM
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
		m.microVMs = msg.microVMs
		m.agent = msg.agent
		m.err = msg.err
		m.lastUpdate = time.Now()
		// Ensure selection is within bounds
		if m.selectedIdx >= len(m.microVMs) {
			m.selectedIdx = len(m.microVMs) - 1
		}
		if m.selectedIdx < 0 {
			m.selectedIdx = 0
		}
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

	// Calculate widths - use full terminal width
	availWidth := m.width
	if availWidth < 60 {
		availWidth = 80
	}

	// Use full width for boxes
	boxWidth := (availWidth - 6) / 2
	fullWidth := availWidth - 4

	// Layout
	if availWidth >= 85 {
		// Side by side - use same height for both boxes
		vmBox := m.renderVMBox(boxWidth, 10)
		agentBox := m.renderAgentBox(boxWidth, 10)
		row := lipgloss.JoinHorizontal(lipgloss.Top, vmBox, "  ", agentBox)
		b.WriteString(row)
		b.WriteString("\n\n")
		b.WriteString(m.renderMicroVMBox(fullWidth))
	} else {
		// Stacked
		b.WriteString(m.renderVMBox(fullWidth, 0))
		b.WriteString("\n")
		b.WriteString(m.renderAgentBox(fullWidth, 0))
		b.WriteString("\n")
		b.WriteString(m.renderMicroVMBox(fullWidth))
	}

	// Footer
	b.WriteString("\n\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m dashboardModel) renderVMBox(width int, minLines int) string {
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

	// Pad to minimum lines
	for len(lines) < minLines {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	style := boxStyle
	if m.linuxVM.Running {
		style = activeBoxStyle
	}
	return style.Width(width).Render(content)
}

func (m dashboardModel) renderAgentBox(width int, minLines int) string {
	var lines []string

	lines = append(lines, headerStyle.Render("FC-AGENT"))
	lines = append(lines, "")

	if m.agent.Available {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			statusOK.Render("✓"),
			valueStyle.Render("ONLINE")))
		lines = append(lines, "")

		if m.agent.TotalVMs > 0 {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				statusOK.Render("✓"),
				bracketTextStyle.Render("FIRECRACKER")))
			lines = append(lines, fmt.Sprintf("  %s  %s",
				labelStyle.Render("VMs"),
				valueStyle.Render(fmt.Sprintf("%d running / %d total", m.agent.RunningVMs, m.agent.TotalVMs))))
		} else {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				labelStyle.Render("○"),
				labelStyle.Render("NO MICROVMS")))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			statusErr.Render("✗"),
			labelStyle.Render("OFFLINE")))
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("  agent not responding"))
	}

	// Pad to minimum lines
	for len(lines) < minLines {
		lines = append(lines, "")
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

	// Header with count
	header := "MICROVMS"
	if len(m.microVMs) > 0 {
		running := 0
		for _, vm := range m.microVMs {
			if vm.Running {
				running++
			}
		}
		header = fmt.Sprintf("MICROVMS  %s",
			labelStyle.Render(fmt.Sprintf("(%d/%d running)", running, len(m.microVMs))))
	}
	lines = append(lines, headerStyle.Render(header))
	lines = append(lines, "")

	if len(m.microVMs) == 0 {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			labelStyle.Render("○"),
			labelStyle.Render("NO MICROVMS")))
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("  run 'fc-macos run' to start"))

		return boxStyle.Width(width).Render(strings.Join(lines, "\n"))
	}

	// Column headers (6 spaces = 2 selector + 1 expand + 1 space + 1 status icon + 1 space)
	colHeader := fmt.Sprintf("      %-12s %-10s %-6s %-8s",
		"NAME", "STATUS", "VCPUS", "MEMORY")
	lines = append(lines, labelStyle.Render(colHeader))
	lines = append(lines, labelStyle.Render("  "+strings.Repeat("─", width-8)))

	// Determine visible range
	endIdx := m.listOffset + m.maxVisible
	if endIdx > len(m.microVMs) {
		endIdx = len(m.microVMs)
	}

	// Show scroll indicator at top
	if m.listOffset > 0 {
		lines = append(lines, labelStyle.Render("  ▲ more above"))
	}

	// Render visible VMs
	for i := m.listOffset; i < endIdx; i++ {
		vm := m.microVMs[i]
		isSelected := i == m.selectedIdx
		isExpanded := m.expandedVMs[vm.ID]

		// Selection indicator
		selector := "  "
		if isSelected {
			selector = statusOK.Render("> ")
		}

		// Expand/collapse indicator
		expandIcon := "▸"
		if isExpanded {
			expandIcon = "▾"
		}

		// Status indicator and text
		var statusText string
		statusIcon := labelStyle.Render("○")
		if vm.Running {
			statusText = "running"
			statusIcon = statusOK.Render("●")
		} else {
			statusText = "stopped"
		}

		// Name (truncate if needed)
		name := vm.Name
		if len(name) > 12 {
			name = name[:10] + ".."
		}

		// Format the row
		var row string
		if isSelected {
			row = fmt.Sprintf("%s%s %s %s %-10s %-6d %-8d",
				selector, labelStyle.Render(expandIcon), statusIcon,
				valueStyle.Width(12).Render(name),
				statusText, vm.VCPUs, vm.MemoryMiB)
			lines = append(lines, valueStyle.Render(row))
		} else {
			row = fmt.Sprintf("%s%s %s %-12s %-10s %-6d %-8d",
				selector, labelStyle.Render(expandIcon), statusIcon, name, statusText, vm.VCPUs, vm.MemoryMiB)
			lines = append(lines, row)
		}

		// Show expanded details
		if isExpanded {
			detailIndent := "      "
			if vm.PID > 0 {
				lines = append(lines, fmt.Sprintf("%s%s %s",
					detailIndent,
					labelStyle.Render("PID:"),
					valueStyle.Render(fmt.Sprintf("%d", vm.PID))))
			}
			if vm.ID != "" {
				idDisplay := vm.ID
				if len(idDisplay) > 40 {
					idDisplay = idDisplay[:38] + ".."
				}
				lines = append(lines, fmt.Sprintf("%s%s %s",
					detailIndent,
					labelStyle.Render("ID: "),
					valueStyle.Render(idDisplay)))
			}
			// Show resource usage
			if vm.Running {
				cpuStr := fmt.Sprintf("%.1f%%", vm.CPUPercent)
				memStr := fmt.Sprintf("%d MB / %d MB", vm.MemoryUsedM, vm.MemoryMiB)
				lines = append(lines, fmt.Sprintf("%s%s %s    %s %s",
					detailIndent,
					labelStyle.Render("CPU:"),
					valueStyle.Render(cpuStr),
					labelStyle.Render("RAM:"),
					valueStyle.Render(memStr)))
			}
			lines = append(lines, "") // Empty line after details
		}
	}

	// Show scroll indicator at bottom
	if endIdx < len(m.microVMs) {
		lines = append(lines, labelStyle.Render("  ▼ more below"))
	}

	// Determine box style
	style := boxStyle
	if len(m.microVMs) > 0 {
		style = activeBoxStyle
	}

	return style.Width(width).Render(strings.Join(lines, "\n"))
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
	if len(m.microVMs) > 0 {
		cmds = append(cmds, fmt.Sprintf("%s/%s nav", keyStyle.Render("j"), keyStyle.Render("k")))
		cmds = append(cmds, fmt.Sprintf("%s details", keyStyle.Render("↵")))
	}
	cmds = append(cmds, fmt.Sprintf("%s refresh", keyStyle.Render("r")))
	if len(m.microVMs) > 0 && m.selectedIdx < len(m.microVMs) {
		cmds = append(cmds, fmt.Sprintf("%s stop vm", keyStyle.Render("s")))
	}
	if m.linuxVM.Running {
		cmds = append(cmds, fmt.Sprintf("%s stop linux", keyStyle.Render("S")))
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
		result.agent, result.microVMs = m.checkAgentAndMicroVMs(ctx, result.linuxVM.IP)
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

func (m dashboardModel) checkAgentAndMicroVMs(ctx context.Context, vmIP string) (agentStatus, []microVMStatus) {
	agent := agentStatus{}
	var vms []microVMStatus

	client := &http.Client{Timeout: 2 * time.Second}
	agentURL := fmt.Sprintf("http://%s:8080", vmIP)

	// Check agent health
	resp, err := client.Get(agentURL + "/health")
	if err != nil {
		return agent, vms
	}
	resp.Body.Close()
	agent.Available = resp.StatusCode == 200

	if !agent.Available {
		return agent, vms
	}

	// Fetch microVMs list from new API
	resp, err = client.Get(agentURL + "/agent/microvms")
	if err != nil {
		return agent, vms
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return agent, vms
	}

	var vmList []MicroVMInfo
	if err := json.NewDecoder(resp.Body).Decode(&vmList); err != nil {
		return agent, vms
	}

	// Convert to dashboard status format
	for _, vm := range vmList {
		vmStatus := microVMStatus{
			ID:          vm.ID,
			Name:        vm.Name,
			Running:     vm.Running,
			PID:         vm.PID,
			CPUPercent:  vm.CPUPercent,
			MemoryUsedM: vm.MemoryUsedMB,
		}
		if vm.Config != nil {
			vmStatus.VCPUs = vm.Config.VCPUs
			vmStatus.MemoryMiB = vm.Config.MemoryMiB
		}
		vms = append(vms, vmStatus)

		if vm.Running {
			agent.RunningVMs++
		}
	}
	agent.TotalVMs = len(vms)
	agent.FirecrackerRunning = agent.RunningVMs > 0

	return agent, vms
}

func (m dashboardModel) stopSelectedMicroVM() tea.Msg {
	if m.linuxVM.IP == "" {
		return actionResultMsg{action: "stop-microvm", err: fmt.Errorf("VM IP not available")}
	}

	if m.selectedIdx >= len(m.microVMs) {
		return actionResultMsg{action: "stop-microvm", err: fmt.Errorf("no microVM selected")}
	}

	selectedVM := m.microVMs[m.selectedIdx]

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://%s:8080/agent/microvms/%s", m.linuxVM.IP, selectedVM.ID)
	req, _ := http.NewRequest("DELETE", url, nil)
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
