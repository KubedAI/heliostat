import { LogoMark } from '@/components/logo';

/**
 * Animated architecture overview: Heliostat as the one console in front of Ray clusters running in
 * many Kubernetes clusters. Pure SVG (SMIL motion), rendered on the server; colors are theme tokens.
 */

type RayKind = 'job' | 'cluster' | 'service';

const LEGEND = [
  { color: 'var(--blue)', label: 'RayJob' },
  { color: 'var(--coral)', label: 'RayCluster' },
  { color: 'var(--mint)', label: 'RayService' },
  { color: 'var(--ink-faint)', label: 'on-prem extension' },
];

const RAY_COLORS: Record<RayKind, string> = {
  job: 'var(--blue)',
  cluster: 'var(--coral)',
  service: 'var(--mint)',
};

const RAY_LABELS: Record<RayKind, string> = {
  job: 'RayJob',
  cluster: 'RayCluster',
  service: 'RayService',
};

/** A Ray cluster: a head node with workers around it. */
function RayGlyph({
  x,
  y,
  kind,
  muted,
  delay,
}: {
  x: number;
  y: number;
  kind: RayKind;
  muted?: boolean;
  delay: number;
}) {
  const color = muted ? 'var(--ink-faint)' : RAY_COLORS[kind];
  const workers = [
    [-22, 18],
    [22, 18],
    [0, -28],
  ];
  return (
    <g className="ray-glyph" aria-label={RAY_LABELS[kind]}>
      {workers.map(([dx, dy]) => (
        <line
          key={`l${dx}${dy}`}
          x1={x}
          y1={y}
          x2={x + dx}
          y2={y + dy}
          stroke={color}
          strokeWidth={2}
          opacity={0.5}
        />
      ))}
      {workers.map(([dx, dy], index) => (
        <circle
          key={`w${dx}${dy}`}
          className="ray-worker"
          style={{ animationDelay: `${delay + index * 0.4}s` }}
          cx={x + dx}
          cy={y + dy}
          r={7.5}
          fill="var(--surface)"
          stroke={color}
          strokeWidth={2.5}
        />
      ))}
      <circle cx={x} cy={y} r={13} fill={color} />
    </g>
  );
}

function KubernetesMark({ x, y, muted }: { x: number; y: number; muted?: boolean }) {
  const spokes = Array.from({ length: 7 }, (_, i) => (i * 2 * Math.PI) / 7);
  const color = muted ? 'var(--ink-faint)' : 'var(--blue)';
  return (
    <g>
      <circle cx={x} cy={y} r={14} fill={color} />
      <circle cx={x} cy={y} r={6.5} fill="none" stroke="var(--surface)" strokeWidth={2} />
      {spokes.map((angle) => (
        <line
          key={angle}
          x1={x + Math.sin(angle) * 6.5}
          y1={y - Math.cos(angle) * 6.5}
          x2={x + Math.sin(angle) * 11}
          y2={y - Math.cos(angle) * 11}
          stroke="var(--surface)"
          strokeWidth={2}
          strokeLinecap="round"
        />
      ))}
    </g>
  );
}

type ClusterBoxProps = {
  x: number;
  title: string;
  subtitle: string;
  kinds: RayKind[];
  muted?: boolean;
  history?: boolean;
  badge?: string;
  width?: number;
};

const BOX_Y = 352;
const BOX_HEIGHT = 290;

function ClusterBox({
  x,
  title,
  subtitle,
  kinds,
  muted,
  history,
  badge,
  width = 260,
}: ClusterBoxProps) {
  const spacing = width / (kinds.length + 1);
  return (
    <g className={muted ? 'arch-cluster muted' : 'arch-cluster'}>
      {!muted && (
        <rect x={x + 5} y={BOX_Y + 5} width={width} height={BOX_HEIGHT} className="arch-shadow" />
      )}
      <rect
        x={x}
        y={BOX_Y}
        width={width}
        height={BOX_HEIGHT}
        className={muted ? 'arch-box dashed' : 'arch-box'}
      />
      <KubernetesMark x={x + 30} y={BOX_Y + 32} muted={muted} />
      <text x={x + 54} y={BOX_Y + 30} className="arch-title">
        {title}
      </text>
      <text x={x + 54} y={BOX_Y + 50} className="arch-mono">
        {subtitle}
      </text>
      {badge && (
        <g>
          <rect x={x + width - 66} y={BOX_Y + 18} width={52} height={24} className="arch-badge" />
          <text x={x + width - 40} y={BOX_Y + 35} textAnchor="middle" className="arch-badge-text">
            {badge}
          </text>
        </g>
      )}
      {kinds.map((kind, index) => (
        <RayGlyph
          key={kind + index}
          x={x + spacing * (index + 1)}
          y={BOX_Y + 135}
          kind={kind}
          muted={muted}
          delay={(x / 100 + index) % 3}
        />
      ))}
      {history && (
        <g>
          <rect x={x + 20} y={BOX_Y + 226} width={width - 40} height={40} className="arch-pill" />
          <ArchiveIcon x={x + 44} y={BOX_Y + 246} />
          <text x={x + 64} y={BOX_Y + 251} className="arch-mono strong">
            Ray History Server
          </text>
        </g>
      )}
    </g>
  );
}

function ArchiveIcon({ x, y }: { x: number; y: number }) {
  return (
    <g stroke="var(--ink)" strokeWidth={2} fill="none">
      <rect x={x - 10} y={y - 9} width={20} height={6} />
      <path d={`M${x - 8} ${y - 3} v12 h16 v-12`} />
      <line x1={x - 3} y1={y + 2} x2={x + 3} y2={y + 2} />
    </g>
  );
}

function MonitorIcon({ x, y }: { x: number; y: number }) {
  return (
    <g stroke="var(--ink)" strokeWidth={2} fill="none">
      <rect x={x - 12} y={y - 10} width={24} height={16} />
      <line x1={x} y1={y + 6} x2={x} y2={y + 11} />
      <line x1={x - 6} y1={y + 11} x2={x + 6} y2={y + 11} />
      <polyline
        points={`${x - 8},${y} ${x - 3},${y - 5} ${x + 1},${y - 1} ${x + 8},${y - 7}`}
        stroke="var(--mint)"
      />
    </g>
  );
}

function PeopleIcon({ x, y }: { x: number; y: number }) {
  return (
    <g fill="var(--ink)">
      <circle cx={x - 9} cy={y - 8} r={6} />
      <path d={`M${x - 20} ${y + 12} a11 11 0 0 1 22 0 z`} />
      <circle cx={x + 10} cy={y - 10} r={7} fill="var(--blue)" />
      <path d={`M${x - 3} ${y + 12} a13 13 0 0 1 26 0 z`} fill="var(--blue)" />
    </g>
  );
}

/** A connector: a faint base line, a flowing dash overlay, and packets that travel along it. */
function Flow({
  id,
  d,
  colors,
  duration,
  muted,
}: {
  id: string;
  d: string;
  colors: string[];
  duration: number;
  muted?: boolean;
}) {
  return (
    <g>
      <path id={id} d={d} className={muted ? 'arch-link muted' : 'arch-link'} />
      <path
        d={d}
        className={muted ? 'arch-flow muted' : 'arch-flow'}
        style={{ animationDuration: `${duration / 3}s` }}
      />
      {colors.map((color, index) => (
        <circle key={index} r={muted ? 4.5 : 5.5} fill={color} className="arch-packet">
          <animateMotion
            dur={`${duration}s`}
            begin={`${(index * duration) / colors.length}s`}
            repeatCount="indefinite"
            keyPoints="0;1"
            keyTimes="0;1"
            calcMode="linear"
          >
            <mpath href={`#${id}`} />
          </animateMotion>
        </circle>
      ))}
    </g>
  );
}

export function ArchitectureDiagram() {
  return (
    <figure className="arch">
      <svg
        viewBox="0 0 1200 680"
        role="img"
        aria-labelledby="arch-title arch-desc"
        className="arch-svg"
      >
        <title id="arch-title">Heliostat architecture</title>
        <desc id="arch-desc">
          Platform and application teams use Heliostat, one read-only console that collects Ray
          jobs, Ray clusters, and Ray Serve endpoints from Amazon EKS clusters in several regions
          and accounts and from on-premises Kubernetes. Every job opens its live Ray Dashboard or
          its Ray History Server archive.
        </desc>

        {/* Flows from every Kubernetes cluster into Heliostat */}
        <Flow
          id="flow-west"
          d="M180 352 C 180 270, 500 290, 500 196"
          colors={['var(--blue)', 'var(--accent)', 'var(--mint)']}
          duration={3.6}
        />
        <Flow
          id="flow-east"
          d="M460 352 C 460 280, 565 290, 565 196"
          colors={['var(--coral)', 'var(--blue)']}
          duration={3}
        />
        <Flow
          id="flow-eu"
          d="M740 352 C 740 280, 635 290, 635 196"
          colors={['var(--accent)', 'var(--mint)']}
          duration={3.3}
        />
        <Flow
          id="flow-onprem"
          d="M1045 352 C 1045 250, 700 290, 700 196"
          colors={['var(--ink-faint)']}
          duration={5}
          muted
        />

        {/* Teams into Heliostat, Heliostat out to Ray dashboards */}
        <Flow
          id="flow-users"
          d="M232 116 H 418"
          colors={['var(--ink)', 'var(--blue)']}
          duration={2.6}
        />
        <Flow
          id="flow-live"
          d="M782 96 C 850 96, 860 68, 928 68"
          colors={['var(--mint)']}
          duration={2.2}
        />
        <Flow
          id="flow-history"
          d="M782 140 C 850 140, 860 160, 928 160"
          colors={['var(--coral)']}
          duration={2.6}
        />

        {/* Teams */}
        <rect x={60} y={72} width={172} height={88} className="arch-box soft" />
        <PeopleIcon x={100} y={112} />
        <text x={132} y={108} className="arch-title small">
          Platform
        </text>
        <text x={132} y={128} className="arch-mono">
          + app teams
        </text>

        {/* Heliostat hub */}
        <rect x={420} y={34} width={360} height={162} className="arch-hub-glow" />
        <rect x={426} y={40} width={360} height={156} className="arch-shadow" />
        <rect x={420} y={34} width={360} height={156} className="arch-box hub" />
        <LogoMark x={444} y={56} size={44} />
        <text x={500} y={78} className="arch-title large">
          Heliostat
        </text>
        <text x={500} y={98} className="arch-mono">
          one read-only console
        </text>
        {[
          ['Jobs', 444, 72],
          ['Clusters', 526, 98],
          ['Endpoints', 634, 122],
        ].map(([label, x, width]) => (
          <g key={label as string}>
            <rect
              x={x as number}
              y={134}
              width={width as number}
              height={34}
              className="arch-tab"
            />
            <text
              x={(x as number) + (width as number) / 2}
              y={156}
              textAnchor="middle"
              className="arch-tab-text"
            >
              {label}
            </text>
          </g>
        ))}

        {/* Deep-link destinations */}
        <rect x={930} y={36} width={236} height={64} className="arch-box" />
        <MonitorIcon x={960} y={68} />
        <text x={986} y={64} className="arch-title small">
          Ray Dashboard
        </text>
        <text x={986} y={84} className="arch-mono live">
          ● live
        </text>
        <rect x={930} y={128} width={236} height={64} className="arch-box" />
        <ArchiveIcon x={960} y={160} />
        <text x={986} y={156} className="arch-title small">
          Ray History Server
        </text>
        <text x={986} y={176} className="arch-mono">
          ◷ archived in S3
        </text>

        {/* AWS boundary */}
        <rect x={32} y={300} width={856} height={366} className="arch-group" />
        <rect x={50} y={288} width={232} height={26} className="arch-group-label" />
        <text x={62} y={306} className="arch-mono strong">
          AWS · accounts × regions
        </text>

        <ClusterBox
          x={50}
          title="Amazon EKS"
          subtitle="us-west-2"
          kinds={['job', 'cluster', 'service']}
          history
          badge="hub"
        />
        <ClusterBox
          x={330}
          title="Amazon EKS"
          subtitle="us-east-1"
          kinds={['job', 'cluster']}
          history
        />
        <ClusterBox
          x={610}
          title="Amazon EKS"
          subtitle="eu-west-1"
          kinds={['job', 'service']}
          history
        />

        {/* On-premises extension */}
        <rect x={906} y={300} width={278} height={366} className="arch-group muted" />
        <rect x={924} y={288} width={120} height={26} className="arch-group-label muted" />
        <text x={936} y={306} className="arch-mono">
          on-premises
        </text>
        <ClusterBox
          x={915}
          width={260}
          title="Kubernetes"
          subtitle="data center"
          kinds={['job', 'cluster']}
          muted
        />
      </svg>
      <figcaption className="arch-legend">
        {LEGEND.map((item) => (
          <span key={item.label}>
            <i style={{ background: item.color }} />
            {item.label}
          </span>
        ))}
      </figcaption>
    </figure>
  );
}
