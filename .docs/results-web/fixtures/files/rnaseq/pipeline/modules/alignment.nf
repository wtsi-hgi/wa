process ALIGN_STAR {
  input:
  path reads

  output:
  path "aligned.bam"

  script:
  """
  echo aligning > aligned.bam
  """
}
