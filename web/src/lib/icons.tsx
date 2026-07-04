// Central icon shim. Maps the app's icon names (formerly lucide-react) to
// Phosphor icons, so every call site stays unchanged - only the import path
// moved to "@/lib/icons". Each icon is wrapped to carry a stable, lib-agnostic
// identity class ("icon-trophy", "icon-refresh-cw", ...) merged with any caller
// className, so tests/styles can target a specific glyph without depending on
// the underlying icon library. `displayName` is the icon's app name (e.g.
// "Swords") for DevTools and name-based tests. The per-theme icon *weight* is
// applied separately by ThemedIconProvider via Phosphor's IconContext. To remap
// a glyph, change its Ph.* source below in one place.

import type { Icon, IconProps } from "@phosphor-icons/react";
import * as Ph from "@phosphor-icons/react";
import { forwardRef } from "react";

export type {
	Icon as LucideIcon,
	IconProps as LucideProps,
} from "@phosphor-icons/react";

const kebab = (s: string) =>
	s
		.replace(/([a-z0-9])([A-Z])/g, "$1-$2")
		.replace(/([A-Z]+)([A-Z][a-z])/g, "$1-$2")
		.toLowerCase();

function withId(PhIcon: Icon, name: string): Icon {
	const cls = `icon-${kebab(name)}`;
	const Wrapped = forwardRef<SVGSVGElement, IconProps>(
		({ className, ...rest }, ref) => (
			<PhIcon
				ref={ref}
				className={className ? `${cls} ${className}` : cls}
				{...rest}
			/>
		),
	);
	Wrapped.displayName = name;
	return Wrapped as unknown as Icon;
}

export const RotateCcw = withId(Ph.ArrowCounterClockwiseIcon, "RotateCcw");
export const ExternalLink = withId(Ph.ArrowSquareOutIcon, "ExternalLink");
export const RefreshCw = withId(Ph.ArrowsClockwiseIcon, "RefreshCw");
export const Maximize2 = withId(Ph.ArrowsOutIcon, "Maximize2");
export const ArrowUpRight = withId(Ph.ArrowUpRightIcon, "ArrowUpRight");
export const Bell = withId(Ph.BellIcon, "Bell");
export const BookOpen = withId(Ph.BookOpenIcon, "BookOpen");
export const Braces = withId(Ph.BracketsCurlyIcon, "Braces");
export const Brain = withId(Ph.BrainIcon, "Brain");
export const Calendar = withId(Ph.CalendarIcon, "Calendar");
export const CalendarDays = withId(Ph.CalendarDotsIcon, "CalendarDays");
export const CalendarPlus = withId(Ph.CalendarPlusIcon, "CalendarPlus");
export const ChevronDown = withId(Ph.CaretDownIcon, "ChevronDown");
export const ChevronLeft = withId(Ph.CaretLeftIcon, "ChevronLeft");
export const ChevronRight = withId(Ph.CaretRightIcon, "ChevronRight");
export const ChevronUp = withId(Ph.CaretUpIcon, "ChevronUp");
export const MessageSquare = withId(Ph.ChatIcon, "MessageSquare");
export const MessagesSquare = withId(Ph.ChatsIcon, "MessagesSquare");
export const Check = withId(Ph.CheckIcon, "Check");
export const CheckCircle2 = withId(Ph.CheckCircleIcon, "CheckCircle2");
export const CheckSquare = withId(Ph.CheckSquareIcon, "CheckSquare");
export const Loader2 = withId(Ph.CircleNotchIcon, "Loader2");
export const Clock = withId(Ph.ClockIcon, "Clock");
export const History = withId(Ph.ClockCounterClockwiseIcon, "History");
export const Coins = withId(Ph.CoinsIcon, "Coins");
export const Columns3 = withId(Ph.ColumnsIcon, "Columns3");
export const Copy = withId(Ph.CopyIcon, "Copy");
export const DollarSign = withId(Ph.CurrencyDollarIcon, "DollarSign");
export const Database = withId(Ph.DatabaseIcon, "Database");
export const Dices = withId(Ph.DiceFiveIcon, "Dices");
export const Download = withId(Ph.DownloadIcon, "Download");
export const Eraser = withId(Ph.EraserIcon, "Eraser");
export const Eye = withId(Ph.EyeIcon, "Eye");
export const EyeOff = withId(Ph.EyeSlashIcon, "EyeOff");
export const FastForward = withId(Ph.FastForwardIcon, "FastForward");
export const FileText = withId(Ph.FileTextIcon, "FileText");
export const Fingerprint = withId(Ph.FingerprintIcon, "Fingerprint");
export const Filter = withId(Ph.FunnelIcon, "Filter");
export const Gauge = withId(Ph.GaugeIcon, "Gauge");
export const Settings = withId(Ph.GearIcon, "Settings");
export const GitBranch = withId(Ph.GitBranchIcon, "GitBranch");
export const GitCompare = withId(Ph.GitDiffIcon, "GitCompare");
export const HardDrive = withId(Ph.HardDriveIcon, "HardDrive");
export const Server = withId(Ph.HardDrivesIcon, "Server");
export const Hash = withId(Ph.HashIcon, "Hash");
export const Image = withId(Ph.ImageIcon, "Image");
export const Info = withId(Ph.InfoIcon, "Info");
export const Key = withId(Ph.KeyIcon, "Key");
export const KeyRound = withId(Ph.KeyIcon, "KeyRound");
export const Zap = withId(Ph.LightningIcon, "Zap");
export const ListOrdered = withId(Ph.ListNumbersIcon, "ListOrdered");
export const Search = withId(Ph.MagnifyingGlassIcon, "Search");
export const Mic = withId(Ph.MicrophoneIcon, "Mic");
export const Monitor = withId(Ph.MonitorIcon, "Monitor");
export const AppleLogo = withId(Ph.AppleLogoIcon, "AppleLogo");
export const WindowsLogo = withId(Ph.WindowsLogoIcon, "WindowsLogo");
export const LinuxLogo = withId(Ph.LinuxLogoIcon, "LinuxLogo");
export const Moon = withId(Ph.MoonIcon, "Moon");
export const Box = withId(Ph.PackageIcon, "Box");
export const Palette = withId(Ph.PaletteIcon, "Palette");
export const Send = withId(Ph.PaperPlaneTiltIcon, "Send");
export const Pencil = withId(Ph.PencilIcon, "Pencil");
export const Play = withId(Ph.PlayIcon, "Play");
export const PlugZap = withId(Ph.PlugsConnectedIcon, "PlugZap");
export const Plus = withId(Ph.PlusIcon, "Plus");
export const PowerOff = withId(Ph.PowerIcon, "PowerOff");
export const Activity = withId(Ph.PulseIcon, "Activity");
export const Bot = withId(Ph.RobotIcon, "Bot");
export const ScrollText = withId(Ph.ScrollIcon, "ScrollText");
export const Shield = withId(Ph.ShieldIcon, "Shield");
export const ShieldCheck = withId(Ph.ShieldCheckIcon, "ShieldCheck");
export const ShieldOff = withId(Ph.ShieldSlashIcon, "ShieldOff");
export const ShieldAlert = withId(Ph.ShieldWarningIcon, "ShieldAlert");
export const Shuffle = withId(Ph.ShuffleIcon, "Shuffle");
export const LogOut = withId(Ph.SignOutIcon, "LogOut");
export const LogIn = withId(Ph.SignInIcon, "LogIn");
export const GithubLogo = withId(Ph.GithubLogoIcon, "GithubLogo");
export const ArrowDownAZ = withId(Ph.SortAscendingIcon, "ArrowDownAZ");
export const ArrowUpZA = withId(Ph.SortDescendingIcon, "ArrowUpZA");
export const Sparkles = withId(Ph.SparkleIcon, "Sparkles");
export const Volume2 = withId(Ph.SpeakerHighIcon, "Volume2");
export const Square = withId(Ph.SquareIcon, "Square");
export const LayoutDashboard = withId(Ph.SquaresFourIcon, "LayoutDashboard");
export const Layers = withId(Ph.StackIcon, "Layers");
export const CircleStop = withId(Ph.StopCircleIcon, "CircleStop");
export const Sun = withId(Ph.SunIcon, "Sun");
export const Swords = withId(Ph.SwordIcon, "Swords");
export const Tag = withId(Ph.TagIcon, "Tag");
export const Target = withId(Ph.TargetIcon, "Target");
export const Terminal = withId(Ph.TerminalIcon, "Terminal");
export const ThumbsDown = withId(Ph.ThumbsDownIcon, "ThumbsDown");
export const ThumbsUp = withId(Ph.ThumbsUpIcon, "ThumbsUp");
export const Timer = withId(Ph.TimerIcon, "Timer");
export const Languages = withId(Ph.TranslateIcon, "Languages");
export const Trash2 = withId(Ph.TrashIcon, "Trash2");
export const TrendingUp = withId(Ph.TrendUpIcon, "TrendingUp");
export const Trophy = withId(Ph.TrophyIcon, "Trophy");
export const Upload = withId(Ph.UploadIcon, "Upload");
export const UserRound = withId(Ph.UserIcon, "UserRound");
export const Users = withId(Ph.UsersIcon, "Users");
export const AlertTriangle = withId(Ph.WarningIcon, "AlertTriangle");
export const AlertCircle = withId(Ph.WarningCircleIcon, "AlertCircle");
export const X = withId(Ph.XIcon, "X");
export const ChevronsDownUp = withId(
	Ph.ArrowsInLineVerticalIcon,
	"ChevronsDownUp",
);
export const ChevronsUpDown = withId(
	Ph.ArrowsOutLineVerticalIcon,
	"ChevronsUpDown",
);
export const SlidersHorizontal = withId(
	Ph.FadersHorizontalIcon,
	"SlidersHorizontal",
);
export const ArrowLeftRight = withId(Ph.ArrowsLeftRightIcon, "ArrowLeftRight");
export const ArrowDownToLine = withId(Ph.ArrowLineDownIcon, "ArrowDownToLine");
export const ArrowUpFromLine = withId(Ph.ArrowLineUpIcon, "ArrowUpFromLine");
