import React from 'react';
import {
	FaBath,
	FaBed,
	FaBolt,
	FaCheck,
	FaChild,
	FaClock,
	FaCouch,
	FaDroplet,
	FaFireFlameCurved,
	FaHouse,
	FaKitchenSet,
	FaMinus,
	FaPen,
	FaPlus,
	FaRulerCombined,
	FaRoad,
	FaShirt,
	FaTemperatureThreeQuarters,
	FaTriangleExclamation,
	FaWarehouse,
	FaXmark
} from 'react-icons/fa6';

const zoneStyleIcons = {
	0: FaCouch,
	1: FaBath,
	2: FaKitchenSet,
	3: FaChild,
	4: FaBed,
	5: FaShirt,
	6: FaRoad,
	7: FaWarehouse
};

export function getZoneIcon(iconId) {
	return zoneStyleIcons[Number(iconId)] || FaHouse;
}

export function formatTemp(value) {
	return value == null || !Number.isFinite(Number(value)) ? '—' : `${Number(value).toFixed(1)}°`;
}

export function formatHumidity(value) {
	return value == null || !Number.isFinite(Number(value)) ? '—' : `${Math.round(Number(value))}%`;
}

export function zoneIsEnabled(zone) {
	return String(zone?.zone_state || '').trim() !== 'zoneOff';
}

export function zoneStateTone(zone) {
	const raw = String(zone?.zone_state || '').trim();
	if (!raw || raw === 'noAlarm') return 'ok';
	if (raw === 'zoneOff') return '';
	return 'danger';
}

export function relayTone(zone) {
	return String(zone?.relay_state || '').trim() === 'on' ? 'ok' : '';
}

export function MetricPill({ icon: Icon, label, value, emphasis = false }) {
	return (
		<div className={`hn-metric-pill${emphasis ? ' is-emphasis' : ''}`}>
			<span className="hn-metric-pill__icon">{Icon ? <Icon /> : null}</span>
			<span className="hn-metric-pill__label">{label}</span>
			<span className="hn-metric-pill__value">{value}</span>
		</div>
	);
}

export function StatusBadge({ icon: Icon, label, value, tone = '' }) {
	return (
		<span className={`hn-badge${tone ? ` ${tone}` : ''}`}>
			{Icon ? <Icon /> : null}
			<span>{label}: {value}</span>
		</span>
	);
}

export function AppleToggle({ checked, onChange, disabled, label }) {
	return (
		<label className={`hn-switch${disabled ? ' is-disabled' : ''}`}>
			<span className="hn-switch__label">{label}</span>
			<input
				type="checkbox"
				checked={checked}
				disabled={disabled}
				onChange={(e) => onChange?.(e.target.checked)}
			/>
			<span className="hn-switch__track">
				<span className="hn-switch__thumb" />
			</span>
		</label>
	);
}

export function StepperInput({
	icon: Icon,
	label,
	unit,
	value,
	onChange,
	min,
	max,
	step = 1,
	disabled,
	width = 168,
	precision = 0
}) {
	const adjust = (delta) => {
		const current = Number(value || 0);
		const next = Number.isFinite(current) ? current + delta : delta;
		const clamped = Math.min(max, Math.max(min, next));
		onChange?.(precision > 0 ? clamped.toFixed(precision) : String(Math.round(clamped)));
	};

	return (
		<label className="hn-stepper" style={{ width }}>
			<span className="hn-stepper__label">
				{Icon ? <Icon /> : null}
				<span>{label}</span>
			</span>
			<span className="hn-stepper__control">
				<button type="button" className="hn-stepper__btn" onClick={() => adjust(-step)} disabled={disabled} aria-label={`Decrease ${label}`}>
					<FaMinus />
				</button>
				<input
					className="hn-stepper__input"
					type="number"
					inputMode="decimal"
					min={min}
					max={max}
					step={step}
					value={value}
					disabled={disabled}
					onChange={(e) => onChange?.(e.target.value)}
				/>
				<span className="hn-stepper__unit">{unit}</span>
				<button type="button" className="hn-stepper__btn" onClick={() => adjust(step)} disabled={disabled} aria-label={`Increase ${label}`}>
					<FaPlus />
				</button>
			</span>
		</label>
	);
}

export function ZoneNameEditor({
	icon: Icon,
	value,
	editing,
	draft,
	busy,
	maxLength = 12,
	onEdit,
	onDraftChange,
	onSave,
	onCancel
}) {
	if (editing) {
		return (
			<div className="hn-zone-name-editor">
				<span className="hn-zone-name-editor__icon">{Icon ? <Icon /> : null}</span>
				<input
					className="hn-zone-name-editor__input"
					value={draft}
					maxLength={maxLength}
					disabled={busy}
					onChange={(e) => onDraftChange?.(e.target.value.slice(0, maxLength))}
				/>
				<span className="hn-zone-name-editor__count">{draft.length}/{maxLength}</span>
				<button type="button" className="hn-icon-btn primary" disabled={busy || !draft.trim()} onClick={onSave} aria-label="Save zone name">
					<FaCheck />
				</button>
				<button type="button" className="hn-icon-btn" disabled={busy} onClick={onCancel} aria-label="Cancel zone rename">
					<FaXmark />
				</button>
			</div>
		);
	}

	return (
		<div className="hn-zone-title-row">
			<span className="hn-zone-title-row__icon">{Icon ? <Icon /> : null}</span>
			<span className="hn-zone-title-row__text" title={value}>{value}</span>
			<button type="button" className="hn-icon-btn" onClick={onEdit} aria-label="Edit zone name">
				<FaPen />
			</button>
		</div>
	);
}

export const ZoneIcons = {
	temperature: FaTemperatureThreeQuarters,
	humidity: FaDroplet,
	hold: FaClock,
	relay: FaBolt,
	mode: FaRulerCombined,
	state: FaTriangleExclamation,
	heat: FaFireFlameCurved,
};
