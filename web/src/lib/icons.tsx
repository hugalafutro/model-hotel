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

export const RotateCcw = withId(Ph.ArrowCounterClockwise, "RotateCcw");
export const ExternalLink = withId(Ph.ArrowSquareOut, "ExternalLink");
export const RefreshCw = withId(Ph.ArrowsClockwise, "RefreshCw");
export const Maximize2 = withId(Ph.ArrowsOut, "Maximize2");
export const ArrowUpRight = withId(Ph.ArrowUpRight, "ArrowUpRight");
export const BookOpen = withId(Ph.BookOpen, "BookOpen");
export const Braces = withId(Ph.BracketsCurly, "Braces");
export const Brain = withId(Ph.Brain, "Brain");
export const Calendar = withId(Ph.Calendar, "Calendar");
export const CalendarDays = withId(Ph.CalendarDots, "CalendarDays");
export const CalendarPlus = withId(Ph.CalendarPlus, "CalendarPlus");
export const ChevronDown = withId(Ph.CaretDown, "ChevronDown");
export const ChevronLeft = withId(Ph.CaretLeft, "ChevronLeft");
export const ChevronRight = withId(Ph.CaretRight, "ChevronRight");
export const ChevronUp = withId(Ph.CaretUp, "ChevronUp");
export const MessageSquare = withId(Ph.Chat, "MessageSquare");
export const MessagesSquare = withId(Ph.Chats, "MessagesSquare");
export const Check = withId(Ph.Check, "Check");
export const CheckCircle2 = withId(Ph.CheckCircle, "CheckCircle2");
export const CheckSquare = withId(Ph.CheckSquare, "CheckSquare");
export const Loader2 = withId(Ph.CircleNotch, "Loader2");
export const Clock = withId(Ph.Clock, "Clock");
export const History = withId(Ph.ClockCounterClockwise, "History");
export const Coins = withId(Ph.Coins, "Coins");
export const Columns3 = withId(Ph.Columns, "Columns3");
export const Copy = withId(Ph.Copy, "Copy");
export const DollarSign = withId(Ph.CurrencyDollar, "DollarSign");
export const Database = withId(Ph.Database, "Database");
export const Dices = withId(Ph.DiceFive, "Dices");
export const Download = withId(Ph.Download, "Download");
export const Eraser = withId(Ph.Eraser, "Eraser");
export const Eye = withId(Ph.Eye, "Eye");
export const EyeOff = withId(Ph.EyeSlash, "EyeOff");
export const FastForward = withId(Ph.FastForward, "FastForward");
export const FileText = withId(Ph.FileText, "FileText");
export const Fingerprint = withId(Ph.Fingerprint, "Fingerprint");
export const Filter = withId(Ph.Funnel, "Filter");
export const Gauge = withId(Ph.Gauge, "Gauge");
export const Settings = withId(Ph.Gear, "Settings");
export const GitBranch = withId(Ph.GitBranch, "GitBranch");
export const GitCompare = withId(Ph.GitDiff, "GitCompare");
export const HardDrive = withId(Ph.HardDrive, "HardDrive");
export const Server = withId(Ph.HardDrives, "Server");
export const Hash = withId(Ph.Hash, "Hash");
export const Image = withId(Ph.Image, "Image");
export const Info = withId(Ph.Info, "Info");
export const Key = withId(Ph.Key, "Key");
export const KeyRound = withId(Ph.Key, "KeyRound");
export const Zap = withId(Ph.Lightning, "Zap");
export const Search = withId(Ph.MagnifyingGlass, "Search");
export const Mic = withId(Ph.Microphone, "Mic");
export const Monitor = withId(Ph.Monitor, "Monitor");
export const AppleLogo = withId(Ph.AppleLogo, "AppleLogo");
export const WindowsLogo = withId(Ph.WindowsLogo, "WindowsLogo");
export const LinuxLogo = withId(Ph.LinuxLogo, "LinuxLogo");
export const Moon = withId(Ph.Moon, "Moon");
export const Box = withId(Ph.Package, "Box");
export const Palette = withId(Ph.Palette, "Palette");
export const Send = withId(Ph.PaperPlaneTilt, "Send");
export const Pencil = withId(Ph.Pencil, "Pencil");
export const Play = withId(Ph.Play, "Play");
export const PlugZap = withId(Ph.PlugsConnected, "PlugZap");
export const Plus = withId(Ph.Plus, "Plus");
export const PowerOff = withId(Ph.Power, "PowerOff");
export const Activity = withId(Ph.Pulse, "Activity");
export const Bot = withId(Ph.Robot, "Bot");
export const ScrollText = withId(Ph.Scroll, "ScrollText");
export const Shield = withId(Ph.Shield, "Shield");
export const ShieldCheck = withId(Ph.ShieldCheck, "ShieldCheck");
export const ShieldOff = withId(Ph.ShieldSlash, "ShieldOff");
export const ShieldAlert = withId(Ph.ShieldWarning, "ShieldAlert");
export const Shuffle = withId(Ph.Shuffle, "Shuffle");
export const LogOut = withId(Ph.SignOut, "LogOut");
export const ArrowDownAZ = withId(Ph.SortAscending, "ArrowDownAZ");
export const ArrowUpZA = withId(Ph.SortDescending, "ArrowUpZA");
export const Sparkles = withId(Ph.Sparkle, "Sparkles");
export const Volume2 = withId(Ph.SpeakerHigh, "Volume2");
export const Square = withId(Ph.Square, "Square");
export const LayoutDashboard = withId(Ph.SquaresFour, "LayoutDashboard");
export const Layers = withId(Ph.Stack, "Layers");
export const CircleStop = withId(Ph.StopCircle, "CircleStop");
export const Sun = withId(Ph.Sun, "Sun");
export const Swords = withId(Ph.Sword, "Swords");
export const Tag = withId(Ph.Tag, "Tag");
export const Target = withId(Ph.Target, "Target");
export const Terminal = withId(Ph.Terminal, "Terminal");
export const ThumbsDown = withId(Ph.ThumbsDown, "ThumbsDown");
export const ThumbsUp = withId(Ph.ThumbsUp, "ThumbsUp");
export const Timer = withId(Ph.Timer, "Timer");
export const Languages = withId(Ph.Translate, "Languages");
export const Trash2 = withId(Ph.Trash, "Trash2");
export const TrendingUp = withId(Ph.TrendUp, "TrendingUp");
export const Trophy = withId(Ph.Trophy, "Trophy");
export const Upload = withId(Ph.Upload, "Upload");
export const Users = withId(Ph.Users, "Users");
export const AlertTriangle = withId(Ph.Warning, "AlertTriangle");
export const AlertCircle = withId(Ph.WarningCircle, "AlertCircle");
export const X = withId(Ph.X, "X");
export const ChevronsDownUp = withId(Ph.ArrowsInLineVertical, "ChevronsDownUp");
export const ChevronsUpDown = withId(
	Ph.ArrowsOutLineVertical,
	"ChevronsUpDown",
);
export const SlidersHorizontal = withId(
	Ph.FadersHorizontal,
	"SlidersHorizontal",
);
export const ArrowLeftRight = withId(Ph.ArrowsLeftRight, "ArrowLeftRight");
export const ArrowDownToLine = withId(Ph.ArrowLineDown, "ArrowDownToLine");
export const ArrowUpFromLine = withId(Ph.ArrowLineUp, "ArrowUpFromLine");
