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

// Gradient colors
var (
	// Fire gradient for the title
	fireGradient = []string{"#ff0000", "#ff4500", "#ff6b00", "#ff8c00", "#ffa500"}

	// Cyan/blue gradient for Linux VM
	vmGradient = []string{"#00d4ff", "#00b4d8", "#0096c7", "#0077b6", "#023e8a"}

	// Purple/pink gradient for agent
	agentGradient = []string{"#f72585", "#b5179e", "#7209b7", "#560bad", "#480ca8"}

	// Green gradient for running status
	greenGradient = []string{"#00ff87", "#00e676", "#00c853", "#00a843", "#008837"}

	// Orange/yellow gradient for microVM
	microVMGradient = []string{"#ffbe0b", "#fb5607", "#ff006e", "#8338ec", "#3a86ff"}
)

// Styles with glow effects
var (
	// Title with fire effect
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ff6b00")).
			Background(lipgloss.Color("#1a0a00")).
			Padding(0, 2).
			MarginBottom(1)

	// Glow container for title
	titleGlowStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#ff4500")).
			Padding(0, 1).
			MarginBottom(1)

	// Box styles with gradients
	vmBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00d4ff")).
			Padding(1, 2)

	vmBoxActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(lipgloss.Color("#00ff87")).
				Padding(1, 2)

	agentBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#b5179e")).
			Padding(1, 2)

	agentBoxActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(lipgloss.Color("#00ff87")).
				Padding(1, 2)

	microVMBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#ff6b00")).
			Padding(1, 2)

	microVMBoxActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(lipgloss.Color("#00ff87")).
				Padding(1, 2)

	// Labels and values
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)

	// Status indicators with glow
	runningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00ff87")).
			Bold(true)

	stoppedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4757")).
			Bold(true)

	// Headers with gradients
	vmHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4ff"))

	agentHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#f72585"))

	microVMHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffbe0b"))

	// Help text
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			MarginTop(1)

	// Sparkle characters for effects
	sparkles = []string{"✦", "✧", "⋆", "·"}
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
	spinner    spinner.Model
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
	tick       int
}

// Messages
type tickMsg time.Time
type statusUpdateMsg struct {
	linuxVM vmStatus
	microVM microVMStatus
	agent   agentStatus
	err     error
}
type animateMsg time.Time

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
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6b00"))

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
		tea.Every(150*time.Millisecond, func(t time.Time) tea.Msg {
			return animateMsg(t)
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

	case animateMsg:
		m.tick++
		return m, tea.Every(150*time.Millisecond, func(t time.Time) tea.Msg {
			return animateMsg(t)
		})

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

	// Animated fire title
	title := m.renderFireTitle()
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

	// Footer with animation
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m dashboardModel) renderFireTitle() string {
	title := "fc-macos Dashboard"
	var result strings.Builder

	// Add animated sparkles
	sparkle := sparkles[m.tick%len(sparkles)]
	sparkleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fireGradient[m.tick%len(fireGradient)]))

	result.WriteString(sparkleStyle.Render(sparkle))
	result.WriteString(" ")

	// Render each character with gradient
	for i, char := range title {
		colorIdx := (i + m.tick) % len(fireGradient)
		charStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(fireGradient[colorIdx]))
		result.WriteString(charStyle.Render(string(char)))
	}

	result.WriteString(" ")
	result.WriteString(sparkleStyle.Render(sparkle))

	// Wrap in glow box
	return titleGlowStyle.Render("🔥 " + result.String() + " 🔥")
}

func (m dashboardModel) renderLinuxVMBox() string {
	var content strings.Builder

	// Header with gradient
	header := m.renderGradientText("🐧 Linux VM", vmGradient)
	content.WriteString(header)
	content.WriteString("\n\n")

	// Status with animated indicator
	if m.linuxVM.Running {
		indicator := m.renderPulsingDot(greenGradient)
		content.WriteString(fmt.Sprintf("%s %s %s\n",
			labelStyle.Render("Status:"),
			indicator,
			runningStyle.Render("Running")))
	} else {
		content.WriteString(fmt.Sprintf("%s %s %s\n",
			labelStyle.Render("Status:"),
			stoppedStyle.Render("●"),
			stoppedStyle.Render("Stopped")))
	}

	// Details
	if m.linuxVM.Running {
		content.WriteString(fmt.Sprintf("%s %s\n",
			labelStyle.Render("Name:  "),
			valueStyle.Render(m.linuxVM.Name)))
		content.WriteString(fmt.Sprintf("%s %s\n",
			labelStyle.Render("IP:    "),
			m.renderGradientText(m.linuxVM.IP, vmGradient)))
		content.WriteString(fmt.Sprintf("%s %s\n",
			labelStyle.Render("CPUs:  "),
			valueStyle.Render(m.linuxVM.CPUs)))
		content.WriteString(fmt.Sprintf("%s %s\n",
			labelStyle.Render("Memory:"),
			valueStyle.Render(m.linuxVM.Memory)))
	} else {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
		content.WriteString("\n")
		content.WriteString(dimStyle.Render("Run 'fc-macos setup' to start"))
	}

	style := vmBoxStyle
	if m.linuxVM.Running {
		style = vmBoxActiveStyle
	}
	return style.Width(36).Render(content.String())
}

func (m dashboardModel) renderAgentBox() string {
	var content strings.Builder

	// Header with gradient
	header := m.renderGradientText("🤖 fc-agent", agentGradient)
	content.WriteString(header)
	content.WriteString("\n\n")

	// Status
	if m.agent.Available {
		indicator := m.renderPulsingDot(greenGradient)
		content.WriteString(fmt.Sprintf("%s %s %s\n",
			labelStyle.Render("Status:"),
			indicator,
			runningStyle.Render("Online")))
	} else {
		content.WriteString(fmt.Sprintf("%s %s %s\n",
			labelStyle.Render("Status:"),
			stoppedStyle.Render("●"),
			stoppedStyle.Render("Offline")))
	}

	if m.agent.Available {
		if m.agent.FirecrackerRunning {
			fcIndicator := m.renderPulsingDot(greenGradient)
			content.WriteString(fmt.Sprintf("%s %s %s\n",
				labelStyle.Render("Firecracker:"),
				fcIndicator,
				runningStyle.Render("Running")))
		} else {
			content.WriteString(fmt.Sprintf("%s %s %s\n",
				labelStyle.Render("Firecracker:"),
				stoppedStyle.Render("●"),
				stoppedStyle.Render("Stopped")))
		}
		if m.agent.PID > 0 {
			content.WriteString(fmt.Sprintf("%s %s\n",
				labelStyle.Render("FC PID:"),
				valueStyle.Render(fmt.Sprintf("%d", m.agent.PID))))
		}
	} else {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
		content.WriteString("\n")
		content.WriteString(dimStyle.Render("Agent not responding"))
	}

	style := agentBoxStyle
	if m.agent.Available {
		style = agentBoxActiveStyle
	}
	return style.Width(36).Render(content.String())
}

func (m dashboardModel) renderMicroVMBox() string {
	var content strings.Builder

	// Header with gradient
	header := m.renderGradientText("🔥 Firecracker MicroVM", microVMGradient)
	content.WriteString(header)
	content.WriteString("\n\n")

	if !m.agent.FirecrackerRunning {
		content.WriteString(fmt.Sprintf("%s %s\n",
			stoppedStyle.Render("●"),
			stoppedStyle.Render("Not Running")))
		content.WriteString("\n")
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
		content.WriteString(dimStyle.Render("Run 'fc-macos run' to start a microVM"))
		return microVMBoxStyle.Width(76).Render(content.String())
	}

	// Running status with pulsing indicator
	indicator := m.renderPulsingDot(greenGradient)
	content.WriteString(fmt.Sprintf("%s %s\n", indicator, runningStyle.Render("Running")))
	content.WriteString("\n")

	// Fancy resource bars
	content.WriteString(m.renderFancyBar("vCPUs ", m.microVM.VCPUs, 8, vmGradient))
	content.WriteString("\n")
	content.WriteString(m.renderFancyBar("Memory", m.microVM.MemoryMiB, 4096, microVMGradient))
	content.WriteString("\n\n")

	// Details with styled values
	content.WriteString(fmt.Sprintf("%s %s\n",
		labelStyle.Render("vCPUs: "),
		m.renderGradientText(fmt.Sprintf("%d cores", m.microVM.VCPUs), vmGradient)))
	content.WriteString(fmt.Sprintf("%s %s\n",
		labelStyle.Render("Memory:"),
		m.renderGradientText(fmt.Sprintf("%d MiB", m.microVM.MemoryMiB), microVMGradient)))

	return microVMBoxActiveStyle.Width(76).Render(content.String())
}

func (m dashboardModel) renderGradientText(text string, gradient []string) string {
	var result strings.Builder
	for i, char := range text {
		colorIdx := i % len(gradient)
		charStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(gradient[colorIdx]))
		result.WriteString(charStyle.Render(string(char)))
	}
	return result.String()
}

func (m dashboardModel) renderPulsingDot(gradient []string) string {
	colorIdx := m.tick % len(gradient)
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(gradient[colorIdx])).
		Bold(true)
	return style.Render("●")
}

func (m dashboardModel) renderFancyBar(label string, used, max int, gradient []string) string {
	barWidth := 45
	percentage := float64(used) / float64(max)
	if percentage > 1 {
		percentage = 1
	}
	filled := int(percentage * float64(barWidth))

	// Build gradient bar
	var bar strings.Builder
	for i := 0; i < barWidth; i++ {
		if i < filled {
			// Gradient fill
			colorIdx := (i * len(gradient)) / barWidth
			if colorIdx >= len(gradient) {
				colorIdx = len(gradient) - 1
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(gradient[colorIdx]))
			bar.WriteString(style.Render("█"))
		} else {
			// Empty with dim color
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
			bar.WriteString(dimStyle.Render("░"))
		}
	}

	// Percentage with color based on usage
	var pctStyle lipgloss.Style
	pct := int(percentage * 100)
	if pct < 50 {
		pctStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87"))
	} else if pct < 80 {
		pctStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffbe0b"))
	} else {
		pctStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4757"))
	}

	return fmt.Sprintf("%s %s %s",
		labelStyle.Width(7).Render(label),
		bar.String(),
		pctStyle.Render(fmt.Sprintf("%3d%%", pct)))
}

func (m dashboardModel) renderFooter() string {
	var footer strings.Builder

	// Animated update indicator
	updateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	sparkle := sparkles[m.tick%len(sparkles)]
	sparkleColor := lipgloss.NewStyle().Foreground(lipgloss.Color(fireGradient[m.tick%len(fireGradient)]))

	footer.WriteString(updateStyle.Render(fmt.Sprintf("Last update: %s ", m.lastUpdate.Format("15:04:05"))))
	footer.WriteString(sparkleColor.Render(sparkle))

	if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4757"))
		footer.WriteString(errStyle.Render(fmt.Sprintf(" (error: %v)", m.err)))
	}

	footer.WriteString("\n")

	// Help with gradient
	help := "Press "
	rKey := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00d4ff")).
		Bold(true).
		Render("r")
	qKey := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ff4757")).
		Bold(true).
		Render("q")

	footer.WriteString(helpStyle.Render(help))
	footer.WriteString(rKey)
	footer.WriteString(helpStyle.Render(" to refresh • "))
	footer.WriteString(qKey)
	footer.WriteString(helpStyle.Render(" to quit"))

	return footer.String()
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
