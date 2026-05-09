import React from 'react';
import { theme, font } from '../theme';

interface TunnelYAMLProps {
  name: string;
  publicPort: number;
  servicePort?: number;
  opacity?: number;
}

export const TunnelYAML: React.FC<TunnelYAMLProps> = ({
  name,
  publicPort,
  servicePort,
  opacity = 1,
}) => {
  const sp = servicePort ?? publicPort;
  const K = theme.codeKey;   // amber
  const V = theme.codeValue; // light cream

  return (
    <div
      style={{
        background: theme.codeBg,
        borderRadius: 8,
        padding: '10px 14px',
        fontFamily: font.mono,
        fontSize: 11,
        lineHeight: 1.75,
        opacity,
        minWidth: 210,
        flexShrink: 0,
        boxShadow: '0 2px 12px rgba(0,0,0,0.25)',
      }}
    >
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>apiVersion</span>
        <span style={{ color: V }}>: frp.operator.io/v1alpha1</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>kind</span>
        <span style={{ color: V }}>: Tunnel</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>metadata</span>
        <span style={{ color: V }}>:</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        {'  '}<span style={{ color: K }}>name</span>
        <span style={{ color: V }}>{`: ${name}`}</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        <span style={{ color: K }}>spec</span>
        <span style={{ color: V }}>:</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        {'  '}<span style={{ color: K }}>ports</span>
        <span style={{ color: V }}>:</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        {'  - '}<span style={{ color: K }}>publicPort</span>
        <span style={{ color: V }}>{`: ${publicPort}`}</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        {'    '}<span style={{ color: K }}>servicePort</span>
        <span style={{ color: V }}>{`: ${sp}`}</span>
      </div>
      <div style={{ whiteSpace: 'pre' }}>
        {'    '}<span style={{ color: K }}>protocol</span>
        <span style={{ color: V }}>: TCP</span>
      </div>
    </div>
  );
};

/**
 * ArrowZone — pure arrow container between cluster and exit zones.
 * No YAML blocks here; Tunnel CRs now live inside the node (next to their Service).
 * Height matches TrafficArrow's svgH (160px) so arrows are fully contained.
 * Arrow children are absolutely positioned within this zone.
 */
export const MiddleZone: React.FC<{ children?: React.ReactNode }> = ({
  children,
}) => {
  return (
    <div
      style={{
        position: 'relative',
        height: 160,
        alignSelf: 'stretch',
        display: 'flex',
        flexDirection: 'row',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {children}
    </div>
  );
};
