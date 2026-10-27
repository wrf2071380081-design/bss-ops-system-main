export function canManageF12Subject(subjectCanManage: boolean, hasFlowContext: boolean, flowCanManage?: boolean): boolean {
  return hasFlowContext ? flowCanManage === true : subjectCanManage;
}
